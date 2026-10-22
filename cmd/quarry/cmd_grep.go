package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"quarry/internal/engine"
	"quarry/internal/pbo"
	"quarry/internal/ui"
)

const grepHelp = `usage: quarry grep <text> [options]

  searches inside the PBOs directly, nothing gets extracted first.
  binarized configs are searched as text. Case-insensitive unless -s.

  -r, --regex          treat <text> as a regular expression
  -s, --case           case sensitive
  -i, --include LIST   file globs to search (default: scripts, configs, layouts,
                       xml, json, materials and other text files)
  -p, --pbo NAME       only search this PBO
  -C, --context N      lines of context around each match
  -n, --max N          stop printing after N matching lines (default 500)
  -l, --files          only print the file names
  -g, -x, -m           same game options as extract

  quarry grep OnRPC
  quarry grep "class\s+PlayerBase" -r -i *.c
`

var textExts = map[string]bool{
	".c": true, ".cpp": true, ".h": true, ".hpp": true, ".bin": true, ".layout": true, ".xml": true,
	".json": true, ".rvmat": true, ".txt": true, ".csv": true, ".ext": true, ".imageset": true,
	".hlsl": true, ".inc": true, ".ptc": true, ".emat": true, ".conf": true, ".bisurf": true,
	".sqf": true, ".sqs": true, ".ini": true, ".cfg": true, ".fxy": true,
}

type fileHit struct {
	name  string
	lines []string // already formatted
	count int
}

func cmdGrep(args []string) int {
	fs := newFlags("grep")
	var c common
	var include, pboName string
	var regex, cs, filesOnly, help bool
	var ctx, max int
	c.bind(fs)
	stringFlag(fs, &include, "include", "i")
	stringFlag(fs, &pboName, "pbo", "p")
	boolFlag(fs, &regex, "regex", "r")
	boolFlag(fs, &cs, "case", "s")
	boolFlag(fs, &filesOnly, "files", "l")
	boolFlag(fs, &help, "help", "h")
	fs.IntVar(&ctx, "context", 0, "")
	fs.IntVar(&ctx, "C", 0, "")
	fs.IntVar(&max, "max", 500, "")
	fs.IntVar(&max, "n", 500, "")
	pos, err := parseArgs(fs, args)
	ui.Init(c.noColor)
	if err != nil || help || len(pos) != 1 {
		fmt.Print(grepHelp)
		return 0
	}

	expr := pos[0]
	if !regex {
		expr = regexp.QuoteMeta(expr)
	}
	if !cs {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		fail("bad pattern: " + err.Error())
	}

	_, sources := c.resolve()
	if pboName != "" {
		sources = findPBOs(sources, pboName)
		if len(sources) == 0 {
			fail("no PBO named " + pboName)
		}
	}
	archives, bad := openAll(sources)
	warnErrs(bad)
	globs := splitList(include)

	type task struct {
		a *pbo.Archive
		e *pbo.Entry
	}
	var tasks []task
	for _, a := range archives {
		for i := range a.Entries {
			e := &a.Entries[i]
			if len(globs) > 0 {
				if !globMatch(globs, baseName(e.Name)) {
					continue
				}
			} else if !textExts[strings.ToLower(extOf(e.Name))] {
				continue
			}
			tasks = append(tasks, task{a, e})
		}
	}

	start := time.Now()
	var h handles
	defer h.closeAll()
	var mu sync.Mutex
	var hits []fileHit
	ch := make(chan task, 64)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU()*2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scratch := make([]byte, 0, 1<<20)
			for t := range ch {
				f, err := h.get(t.a.Path)
				if err != nil {
					continue
				}
				data, _, err := engine.Load(f, t.e, false, &scratch)
				if err != nil || bytes.IndexByte(data[:min(len(data), 512)], 0) >= 0 {
					continue
				}
				if !re.Match(data) {
					continue
				}
				fh := render(gameName(t.a, t.e.Name), data, re, ctx)
				mu.Lock()
				hits = append(hits, fh)
				mu.Unlock()
			}
		}()
	}
	for _, t := range tasks {
		ch <- t
	}
	close(ch)
	wg.Wait()

	sort.Slice(hits, func(i, j int) bool { return hits[i].name < hits[j].name })
	printed, total := 0, 0
	for _, fh := range hits {
		total += fh.count
		if filesOnly {
			fmt.Println(fh.name)
			continue
		}
		for _, ln := range fh.lines {
			if printed >= max {
				break
			}
			fmt.Println(ln)
			printed++
		}
	}
	msg := fmt.Sprintf("%s matching lines in %d files, searched %s files in %s",
		ui.Commas(int64(total)), len(hits), ui.Commas(int64(len(tasks))), ui.Duration(time.Since(start)))
	if !filesOnly && total > printed {
		msg += fmt.Sprintf("  (showing %d, raise with -n)", printed)
	}
	fmt.Fprintln(os.Stderr, ui.Dim(msg))
	if len(hits) == 0 {
		return 1
	}
	return 0
}

func extOf(n string) string {
	b := baseName(n)
	if i := strings.LastIndexByte(b, '.'); i >= 0 {
		return b[i:]
	}
	return ""
}

// turns a matching file into printable lines, with optional context
func render(name string, data []byte, re *regexp.Regexp, ctx int) fileHit {
	lines := bytes.Split(data, []byte("\n"))
	fh := fileHit{name: name}
	last := -1
	for i, ln := range lines {
		if !re.Match(ln) {
			continue
		}
		fh.count++
		from, to := max(0, i-ctx), min(len(lines)-1, i+ctx)
		if last >= 0 && from > last+1 && ctx > 0 {
			fh.lines = append(fh.lines, ui.Dim("--"))
		}
		for k := max(from, last+1); k <= to; k++ {
			text := strings.TrimRight(string(lines[k]), "\r")
			if len(text) > 300 {
				text = text[:300] + "..."
			}
			if k == i {
				text = re.ReplaceAllStringFunc(text, func(m string) string { return ui.Accent(ui.Bold(m)) })
				fh.lines = append(fh.lines, fmt.Sprintf("%s%s %s", ui.Cyan(name), ui.Dim(fmt.Sprintf(":%d:", k+1)), strings.TrimSpace(text)))
			} else {
				fh.lines = append(fh.lines, fmt.Sprintf("%s%s %s", ui.Dim(name), ui.Dim(fmt.Sprintf("-%d-", k+1)), strings.TrimSpace(text)))
			}
			last = k
		}
	}
	return fh
}
