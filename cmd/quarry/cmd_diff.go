package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"quarry/internal/engine"
	"quarry/internal/steam"
	"quarry/internal/ui"
)

const diffHelp = `usage: quarry diff [destination] [options]

  shows what the last extraction added, changed or removed - run it right after
  a DayZ update to see which scripts and configs the patch touched.
  The destination is the folder you extracted to (default ~/dayz-extract).

  -n, --run N        look at an older run (0 = latest, 1 = the one before...)
  -i, --include LIST only show files matching these globs
      --all          show every file, not just the first 200
      --runs         list the saved runs and exit
`

func cmdDiff(args []string) int {
	fs := newFlags("diff")
	var include string
	var run int
	var all, runs, help, noColor bool
	stringFlag(fs, &include, "include", "i")
	fs.IntVar(&run, "run", 0, "")
	fs.IntVar(&run, "n", 0, "")
	boolFlag(fs, &all, "all", "")
	boolFlag(fs, &runs, "runs", "")
	boolFlag(fs, &help, "help", "h")
	boolFlag(fs, &noColor, "no-color", "")
	pos, err := parseArgs(fs, args)
	ui.Init(noColor)
	if err != nil || help || len(pos) > 1 {
		fmt.Print(diffHelp)
		return 0
	}
	dest := defaultDest()
	if len(pos) == 1 {
		dest = steam.HostPath(pos[0])
	} else if cfg, _, _ := loadConfig(""); cfg.Destination != "" {
		dest = steam.HostPath(cfg.Destination)
	}
	saved, err := engine.LoadRuns(dest)
	if err != nil || len(saved) == 0 {
		fail("no saved changes in " + dest + " yet. Extract there first; changes are recorded when files differ")
	}
	if runs {
		for i, r := range saved {
			fmt.Printf("  %d  %s  %s %s %s\n", i, fmtTime(r.Time), ui.Good(fmt.Sprintf("+%d", r.AddedN)),
				ui.Accent(fmt.Sprintf("~%d", r.ModifiedN)), ui.Bad(fmt.Sprintf("-%d", r.RemovedN)))
		}
		return 0
	}
	if run < 0 || run >= len(saved) {
		fail(fmt.Sprintf("no run %d, there are %d saved (see --runs)", run, len(saved)))
	}
	r := saved[run]
	globs := splitList(include)

	fmt.Println()
	fmt.Printf("  %s  %s\n\n", ui.Bold("Changes"), ui.Dim(fmtTime(r.Time)))
	fmt.Printf("  %s added   %s changed   %s removed\n\n", ui.Good(ui.Commas(int64(r.AddedN))),
		ui.Accent(ui.Commas(int64(r.ModifiedN))), ui.Bad(ui.Commas(int64(r.RemovedN))))

	type item struct{ mark, path string }
	var items []item
	for _, f := range r.Modified {
		items = append(items, item{ui.Accent("~"), f})
	}
	for _, f := range r.Added {
		items = append(items, item{ui.Good("+"), f})
	}
	for _, f := range r.Removed {
		items = append(items, item{ui.Bad("-"), f})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].path < items[j].path })

	// quick rollup by the first two folders so a big update is readable
	groups := map[string]int{}
	var shown []item
	for _, it := range items {
		if len(globs) > 0 && !globMatch(globs, baseName(it.path)) {
			continue
		}
		shown = append(shown, it)
		parts := strings.Split(it.path, "/")
		groups[strings.Join(parts[:min(2, len(parts)-1)], "/")]++
	}
	if len(shown) > 40 {
		type g struct {
			name string
			n    int
		}
		var gs []g
		for k, v := range groups {
			gs = append(gs, g{k, v})
		}
		sort.Slice(gs, func(i, j int) bool { return gs[i].n > gs[j].n })
		fmt.Println("  " + ui.Dim("where it happened"))
		for i, x := range gs {
			if i == 8 {
				break
			}
			fmt.Printf("  %6s  %s\n", ui.Commas(int64(x.n)), x.name)
		}
		fmt.Println()
	}
	limit := 200
	if all {
		limit = len(shown)
	}
	for i, it := range shown {
		if i == limit {
			fmt.Printf("  %s\n", ui.Dim(fmt.Sprintf("... and %d more (--all to show everything)", len(shown)-limit)))
			break
		}
		fmt.Printf("  %s %s\n", it.mark, it.path)
	}
	if r.AddedN > len(r.Added) || r.ModifiedN > len(r.Modified) || r.RemovedN > len(r.Removed) {
		fmt.Fprintln(os.Stderr, ui.Dim("\n  (huge run, only the first few thousand paths per kind are saved)"))
	}
	fmt.Println()
	return 0
}

func fmtTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02 15:04")
}
