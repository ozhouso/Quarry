package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"quarry/internal/pbo"
	"quarry/internal/steam"
	"quarry/internal/ui"
)

// flags every command that touches the game files has
type common struct {
	game, mods, dlc       string
	config                string
	experimental, noColor bool
}

func (c *common) bind(fs *flag.FlagSet) {
	stringFlag(fs, &c.game, "game", "g")
	stringFlag(fs, &c.mods, "mod", "m")
	boolFlag(fs, &c.experimental, "experimental", "x")
	boolFlag(fs, &c.noColor, "no-color", "")
	fs.StringVar(&c.dlc, "dlc", "", "")
	fs.StringVar(&c.config, "config", "", "")
}

// the game folder, from --game or steam
func (c *common) gameDir() string {
	if c.game != "" {
		return hostPath(c.game)
	}
	dir, ok := steam.FindDayZ(c.experimental)
	if !ok {
		fail("couldn't find a DayZ install. Pass one with --game PATH")
	}
	return dir
}

// which dlc to use. set=false means "all of them". flag wins over quarry.json
func (c *common) dlcSelection() (names []string, set bool) {
	if c.dlc != "" {
		if strings.EqualFold(c.dlc, "none") {
			return nil, true
		}
		return splitList(c.dlc), true
	}
	cfg, _, err := loadConfig(c.config)
	if err != nil {
		fail(err.Error())
	}
	if cfg.DLC != nil {
		return *cfg.DLC, true
	}
	return nil, false
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder))
	return fs
}

// go's flag package stops at the first non-flag. we loop so flags can come after
// the positional args too (quarry cat foo.c -p scripts)
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	return pos, nil
}

// finds the game and every pbo we should look at, bails out with a message if it cant
func (c *common) resolve() (string, []string) {
	game := c.gameDir()
	cfg, _, err := loadConfig(c.config)
	if err != nil {
		fail(err.Error())
	}
	dlc, set := c.dlcSelection()
	mods := append(append([]string{}, cfg.Mods...), splitList(c.mods)...)
	sources, err := collect(game, mods, dlc, set)
	if err != nil {
		fail(err.Error())
	}
	if len(sources) == 0 {
		fail("no PBOs found under " + game)
	}
	return game, sources
}

// picks pbos out of the list by file name (scripts), prefix (DZ\AI) or a real path
func findPBOs(sources []string, name string) []string {
	if st, err := os.Stat(name); err == nil && !st.IsDir() {
		return []string{name}
	}
	want := strings.ToLower(strings.TrimSuffix(name, ".pbo"))
	var out []string
	for _, s := range sources {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(s), filepath.Ext(s)))
		if base == want {
			out = append(out, s)
		}
	}
	return out
}

// opens headers for all the paths in parallel
func openAll(paths []string) ([]*pbo.Archive, []error) {
	res := make([]*pbo.Archive, len(paths))
	errs := make([]error, len(paths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for i, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			res[i], errs[i] = pbo.Open(p)
		}()
	}
	wg.Wait()
	var out []*pbo.Archive
	var bad []error
	for i := range res {
		if errs[i] != nil {
			bad = append(bad, errs[i])
		} else {
			out = append(out, res[i])
		}
	}
	return out, bad
}

// one shared read handle per pbo
type handles struct{ m sync.Map }

func (h *handles) get(path string) (*os.File, error) {
	if f, ok := h.m.Load(path); ok {
		return f.(*os.File), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if actual, loaded := h.m.LoadOrStore(path, f); loaded {
		f.Close()
		return actual.(*os.File), nil
	}
	return f, nil
}

func (h *handles) closeAll() {
	h.m.Range(func(_, v any) bool { v.(*os.File).Close(); return true })
}

// the name as the game would see it: DZ\AI\config.bin
func gameName(a *pbo.Archive, name string) string {
	return strings.ReplaceAll(a.Prefix(), "/", `\`) + `\` + name
}

func warnErrs(errs []error) {
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, ui.Bad("warn")+" "+e.Error())
	}
}

func defaultDest() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "dayz-extract")
}

func hostPath(p string) string { return steam.HostPath(p) }
