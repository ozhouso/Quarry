// Package engine does the actual extracting. Everything goes through one shared
// worker pool so a couple of giant pbos dont leave the other cores sitting around
package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quarry/internal/lzss"
	"quarry/internal/pbo"
	"quarry/internal/rap"
)

type Options struct {
	Dest    string
	Sources []string
	Include []string // globs, matched against the file name only
	Exclude []string
	Jobs    int
	Raw     bool // dont convert configs back to text
	Force   bool // skip the manifest, redo everything
}

// Stats gets updated from lots of goroutines so its all atomics
type Stats struct {
	Total, Done, Skipped atomic.Int64 // counts of pbos
	Files                atomic.Int64
	Bytes                atomic.Int64
	Current              atomic.Value // name of the pbo we last queued up
	Same                 atomic.Int64 // files that already matched on disk so we didnt rewrite them
	TotalBytes           atomic.Int64 // packed bytes we plan to read, used for the progress bar
	Processed            atomic.Int64 // packed bytes already dealt with
	exts                 sync.Map     // extension -> *ExtStat
}

type ExtStat struct {
	Ext          string
	Files, Bytes atomic.Int64
}

func (s *Stats) addExt(ext string, size int64) {
	v, _ := s.exts.LoadOrStore(ext, &ExtStat{Ext: ext})
	e := v.(*ExtStat)
	e.Files.Add(1)
	e.Bytes.Add(size)
}

// TopExts returns the n most common file types by count
func (s *Stats) TopExts(n int) []*ExtStat {
	var all []*ExtStat
	s.exts.Range(func(_, v any) bool { all = append(all, v.(*ExtStat)); return true })
	sort.Slice(all, func(i, j int) bool { return all[i].Files.Load() > all[j].Files.Load() })
	if len(all) > n {
		all = all[:n]
	}
	return all
}

type Result struct {
	Errors  []error
	Changes Changeset
}

type manifestPBO struct {
	Size    int64    `json:"size"`
	ModTime int64    `json:"mtime"`
	Files   []string `json:"files"`
}

type manifest struct {
	Filters string                 `json:"filters"`
	PBOs    map[string]manifestPBO `json:"pbos"`
}

const manifestName = ".quarry.json"

type job struct {
	ar *pbo.Archive
	st *archiveState
	e  *pbo.Entry
}

type archiveState struct {
	prefix string
	mu     sync.Mutex
	files  []string
	failed bool

	added, modified []string // paths that were new or different this run
}

func (o *Options) filtersKey() string {
	return fmt.Sprintf("raw=%v|inc=%v|exc=%v", o.Raw, o.Include, o.Exclude)
}

func matchAny(patterns []string, name string) bool {
	base := strings.ToLower(name)
	for _, p := range patterns {
		if ok, _ := path.Match(strings.ToLower(p), base); ok {
			return true
		}
	}
	return false
}

func (o *Options) wanted(entryName string) bool {
	base := entryName
	if i := strings.LastIndexAny(base, `\/`); i >= 0 {
		base = base[i+1:]
	}
	if len(o.Include) > 0 && !matchAny(o.Include, base) {
		return false
	}
	return !matchAny(o.Exclude, base)
}

func loadManifest(dest string) manifest {
	m := manifest{PBOs: map[string]manifestPBO{}}
	if data, err := os.ReadFile(filepath.Join(dest, manifestName)); err == nil {
		_ = json.Unmarshal(data, &m)
		if m.PBOs == nil {
			m.PBOs = map[string]manifestPBO{}
		}
	}
	return m
}

func Run(o Options, stats *Stats) Result {
	var res Result
	if o.Jobs <= 0 {
		o.Jobs = min(runtime.NumCPU()*2, 32)
	}
	if err := os.MkdirAll(o.Dest, 0o755); err != nil {
		return Result{Errors: []error{err}}
	}
	old := loadManifest(o.Dest)
	if old.Filters != o.filtersKey() {
		old.PBOs = map[string]manifestPBO{}
	}
	next := manifest{Filters: o.filtersKey(), PBOs: map[string]manifestPBO{}}
	stats.Total.Store(int64(len(o.Sources)))

	// read all the headers at once, its quick
	type opened struct {
		ar  *pbo.Archive
		err error
	}
	opens := make([]opened, len(o.Sources))
	var wg sync.WaitGroup
	sem := make(chan struct{}, o.Jobs)
	for i, src := range o.Sources {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ar, err := pbo.Open(src)
			opens[i] = opened{ar, err}
		}()
	}
	wg.Wait()

	jobs := make(chan job, o.Jobs*4)
	var errMu sync.Mutex
	addErr := func(err error) {
		errMu.Lock()
		res.Errors = append(res.Errors, err)
		errMu.Unlock()
	}

	dirs := sync.Map{}
	ensureDir := func(d string) error {
		if _, ok := dirs.Load(d); ok {
			return nil
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
		dirs.Store(d, struct{}{})
		return nil
	}

	// counts how many files are left in each pbo so we know when its finished
	type tracker struct {
		pending atomic.Int64
	}
	trackers := map[*archiveState]*tracker{}
	var states []*archiveState
	var archives []*pbo.Archive
	var planned []job

	for _, op := range opens {
		if op.err != nil {
			addErr(op.err)
			stats.Done.Add(1)
			continue
		}
		ar := op.ar
		key := ar.Path
		if prev, ok := old.PBOs[key]; ok && !o.Force && prev.Size == ar.Size && prev.ModTime == ar.ModTime {
			next.PBOs[key] = prev
			stats.Skipped.Add(1)
			stats.Done.Add(1)
			continue
		}
		st := &archiveState{prefix: ar.Prefix()}
		tr := &tracker{}
		trackers[st] = tr
		states = append(states, st)
		archives = append(archives, ar)
		for i := range ar.Entries {
			e := &ar.Entries[i]
			if !o.wanted(e.Name) {
				continue
			}
			planned = append(planned, job{ar, st, e})
			stats.TotalBytes.Add(int64(e.DataSize))
			tr.pending.Add(1)
		}
		if tr.pending.Load() == 0 {
			stats.Done.Add(1)
		}
	}

	finish := func(ar *pbo.Archive, st *archiveState) {
		stats.Done.Add(1)
		if st.failed {
			return // not in the manifest so it gets retried next time
		}
		// delete leftovers from an older run that arent in the pbo anymore
		if prev, ok := old.PBOs[ar.Path]; ok {
			keep := make(map[string]struct{}, len(st.files))
			for _, f := range st.files {
				keep[f] = struct{}{}
			}
			for _, f := range prev.Files {
				if _, ok := keep[f]; !ok {
					_ = os.Remove(filepath.Join(o.Dest, f))
					errMu.Lock()
					res.Changes.addRemoved(f)
					errMu.Unlock()
				}
			}
		}
		errMu.Lock()
		for _, f := range st.added {
			res.Changes.addAdded(f)
		}
		for _, f := range st.modified {
			res.Changes.addModified(f)
		}
		next.PBOs[ar.Path] = manifestPBO{Size: ar.Size, ModTime: ar.ModTime, Files: st.files}
		errMu.Unlock()
	}

	// pbos where the filter matched nothing still need to be marked done
	for i, st := range states {
		if trackers[st].pending.Load() == 0 {
			finish(archives[i], st)
		}
	}

	bufPool := sync.Pool{New: func() any { b := make([]byte, 0, 1<<20); return &b }}
	handles := sync.Map{} // one open handle per pbo, shared between workers
	getFile := func(p string) (*os.File, error) {
		if f, ok := handles.Load(p); ok {
			return f.(*os.File), nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		if actual, loaded := handles.LoadOrStore(p, f); loaded {
			f.Close()
			return actual.(*os.File), nil
		}
		return f, nil
	}

	work := func() {
		for j := range jobs {
			if err := extractOne(&o, j, &bufPool, getFile, ensureDir, stats); err != nil {
				j.st.mu.Lock()
				j.st.failed = true
				j.st.mu.Unlock()
				addErr(fmt.Errorf("%s: %s: %w", filepath.Base(j.ar.Path), j.e.Name, err))
			}
			if trackers[j.st].pending.Add(-1) == 0 {
				finish(j.ar, j.st)
			}
		}
	}
	var wwg sync.WaitGroup
	for i := 0; i < o.Jobs; i++ {
		wwg.Add(1)
		go func() { defer wwg.Done(); work() }()
	}
	// nothing clever here, just queue them in order so the progress looks sensible
	for _, j := range planned {
		stats.Current.Store(filepath.Base(j.ar.Path))
		jobs <- j
	}
	close(jobs)
	wwg.Wait()
	handles.Range(func(_, v any) bool { v.(*os.File).Close(); return true })
	res.Changes.finalize()
	if res.Changes.Total() > 0 {
		_ = appendRun(o.Dest, res.Changes)
	}

	if data, err := json.Marshal(next); err == nil {
		tmp := filepath.Join(o.Dest, manifestName+".tmp")
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, filepath.Join(o.Dest, manifestName))
		}
	}
	return res
}

func extractOne(o *Options, j job, pool *sync.Pool, getFile func(string) (*os.File, error), ensureDir func(string) error, stats *Stats) error {
	defer stats.Processed.Add(int64(j.e.DataSize))
	f, err := getFile(j.ar.Path)
	if err != nil {
		return err
	}
	bp := pool.Get().(*[]byte)
	defer pool.Put(bp)
	data, rel, err := Load(f, j.e, o.Raw, bp)
	if err != nil {
		return err
	}

	outRel := filepath.Join(j.st.prefix, rel)
	full := filepath.Join(o.Dest, outRel)
	if err := ensureDir(filepath.Dir(full)); err != nil {
		return err
	}
	kind, err := writeIfChanged(full, data)
	if err != nil {
		return err
	}
	stats.Files.Add(1)
	stats.Bytes.Add(int64(len(data)))
	ext := strings.ToLower(filepath.Ext(outRel))
	if ext == "" {
		ext = "(none)"
	}
	stats.addExt(ext, int64(len(data)))
	j.st.mu.Lock()
	j.st.files = append(j.st.files, outRel)
	switch kind {
	case wroteNew:
		j.st.added = append(j.st.added, filepath.ToSlash(outRel))
	case wroteChanged:
		j.st.modified = append(j.st.modified, filepath.ToSlash(outRel))
	default:
		stats.Same.Add(1)
	}
	j.st.mu.Unlock()
	return nil
}

const (
	wroteSame = iota
	wroteNew
	wroteChanged
)

// writes the file, but if it was already there with the same bytes we leave it alone.
// O_EXCL means a brand new file costs just one syscall, no stat first
func writeIfChanged(full string, data []byte) (int, error) {
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return wroteNew, werr
	}
	if !errors.Is(err, fs.ErrExist) {
		return 0, err
	}
	if st, serr := os.Stat(full); serr == nil && st.Size() == int64(len(data)) {
		if old, rerr := os.ReadFile(full); rerr == nil && bytes.Equal(old, data) {
			return wroteSame, nil
		}
	}
	return wroteChanged, os.WriteFile(full, data, 0o644)
}

// Load reads one entry out of a pbo, unpacks it and (unless raw) turns binary configs
// back into text. Returns the bytes and the file name to use, config.bin becomes config.cpp.
// The data may point into *scratch so dont keep it around after reusing the buffer
func Load(f *os.File, e *pbo.Entry, raw bool, scratch *[]byte) ([]byte, string, error) {
	rel, ok := pbo.SafeRel(e.Name)
	if !ok {
		return nil, "", fmt.Errorf("unsafe entry name")
	}
	need := int(e.DataSize)
	if e.Method == pbo.MethodCompressed {
		need += int(e.OrigSize)
	}
	buf := *scratch
	if cap(buf) < need {
		buf = make([]byte, 0, need+need/4)
	}
	buf = buf[:need]
	*scratch = buf[:0]

	packed := buf[:e.DataSize]
	if err := readFull(f, packed, e.Offset); err != nil {
		return nil, "", err
	}
	data := packed
	switch e.Method {
	case pbo.MethodRaw:
	case pbo.MethodCompressed:
		data = buf[e.DataSize:]
		if err := lzss.Decode(packed, data); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", fmt.Errorf("unsupported packing method %#x", e.Method)
	}

	if !raw && rap.Is(data) {
		ext := strings.ToLower(filepath.Ext(rel))
		if ext == ".bin" || ext == ".rvmat" || ext == ".bisurf" || ext == ".cpp" {
			if text, err := rap.Decode(data); err == nil {
				data = text
				if ext == ".bin" {
					rel = strings.TrimSuffix(rel, filepath.Ext(rel)) + ".cpp"
				}
			}
		}
	}
	return data, rel, nil
}

// readFull reads in 4MB bites and retries a few times. The windows drive mount in wsl
// will sometimes throw ENOMEM when lots of big reads happen at once, a retry fixes it
func readFull(f *os.File, buf []byte, off int64) error {
	const chunk = 4 << 20
	for len(buf) > 0 {
		n := min(len(buf), chunk)
		var err error
		for try := 0; try < 6; try++ {
			if _, err = f.ReadAt(buf[:n], off); err == nil {
				break
			}
			time.Sleep(time.Duration(try+1) * 40 * time.Millisecond)
		}
		if err != nil {
			return err
		}
		buf = buf[n:]
		off += int64(n)
	}
	return nil
}
