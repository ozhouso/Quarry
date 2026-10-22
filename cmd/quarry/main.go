package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"time"

	"quarry/internal/engine"
	"quarry/internal/steam"
	"quarry/internal/ui"
	"quarry/internal/update"
)

// stamped by the release build with -ldflags "-X main.version=1.2.3"
var version = "1.0.0"

func printHelp() {
	ui.Init(false)
	ui.Banner(version)
	h := func(s string) { fmt.Println("  " + ui.Bold(ui.Accent(s))) }
	row := func(flags, desc string) { fmt.Printf("    %s  %s\n", ui.Cyan(fmt.Sprintf("%-24s", flags)), desc) }

	h("USAGE")
	fmt.Printf("    quarry %s %s\n\n", ui.Dim("[destination]"), ui.Dim("[options]"))
	h("COMMANDS")
	row("quarry [dest]", "extract everything (the default)")
	row("quarry ls [pbo]", "list PBOs, or the files inside one")
	row("quarry cat <file>", "print a file straight from the PBOs")
	row("quarry grep <text>", "search every script/config without extracting")
	row("quarry diff [dest]", "what changed in the last extraction")
	row("quarry dlc", "show detected DLC folders (nasdara, sakhal...)")
	row("quarry init", "create a quarry.json to pick DLC and mods")
	row("quarry update", "install the newest release from GitHub")
	fmt.Printf("    %s\n\n", ui.Dim("run any command with -h for its own help"))
	h("OPTIONS")
	row("-x, --experimental", "use the DayZ Experimental install")
	row("-g, --game PATH", "game folder (skips Steam detection)")
	row("    --dlc LIST", "only these DLC folders (or \"none\"), default: all found")
	row("    --config FILE", "use this quarry.json instead of looking for one")
	row("-i, --include LIST", "only extract files matching these globs")
	row("-e, --exclude LIST", "skip files matching these globs")
	row("-m, --mod LIST", "also extract mods: @Name, a folder, or a .pbo")
	row("-j, --jobs N", "worker count (default: 2 x CPU cores)")
	row("    --raw", "keep binarized configs, don't convert to text")
	row("    --force", "ignore saved state, extract everything again")
	row("    --list", "print the PBOs that would be used, then exit")
	row("-q, --quiet", "no output except errors")
	row("    --no-color", "plain output, no colours")
	row("-v, --version", "show the version")
	row("-h, --help", "show this help")
	fmt.Println()
	h("EXAMPLES")
	ex := func(cmd, note string) { fmt.Printf("    %s\n      %s\n", ui.Good("$ "+cmd), ui.Dim(note)) }
	ex("quarry P:\\", "extract everything to P:\\")
	ex("quarry out -i *.c,*.cpp,*.bin", "scripts and configs only")
	ex("quarry out -e *.paa,*.rvmat -x", "experimental branch, no textures or materials")
	ex("quarry out -m @CF,@Community-Online-Tools", "include workshop mods too")
	fmt.Println()
	fmt.Printf("    %s\n\n", ui.Dim("destination defaults to ~/dayz-extract. Re-runs only touch PBOs that changed."))
	fmt.Printf("    %s\n\n", ui.Dim("created by ozzie"))
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// registers the same flag twice, once long once short (go's flag pkg has no aliases)
func stringFlag(fs *flag.FlagSet, p *string, long, short string) {
	fs.StringVar(p, long, "", "")
	fs.StringVar(p, short, "", "")
}

func boolFlag(fs *flag.FlagSet, p *bool, long, short string) {
	fs.BoolVar(p, long, false, "")
	if short != "" {
		fs.BoolVar(p, short, false, "")
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, ui.Bad("error")+" "+msg)
	ui.Exit(1)
}

func main() {
	update.Cleanup()
	if len(os.Args) > 1 {
		cmds := map[string]func([]string) int{
			"update": cmdUpdate, "ls": cmdLs, "cat": cmdCat, "grep": cmdGrep, "diff": cmdDiff, "dlc": cmdDLC, "init": cmdInit,
		}
		if fn, ok := cmds[os.Args[1]]; ok {
			os.Exit(fn(os.Args[2:]))
		}
		if os.Args[1] == "extract" {
			os.Args = append(os.Args[:1], os.Args[2:]...)
		}
	}
	os.Exit(runExtract())
}

func runExtract() int {
	fs := newFlags("quarry")
	var c common
	var include, exclude string
	var raw, force, list, quiet, help, ver bool
	var jobs int
	c.bind(fs)
	stringFlag(fs, &include, "include", "i")
	stringFlag(fs, &exclude, "exclude", "e")
	boolFlag(fs, &raw, "raw", "")
	boolFlag(fs, &force, "force", "")
	boolFlag(fs, &list, "list", "")
	boolFlag(fs, &quiet, "quiet", "q")
	boolFlag(fs, &help, "help", "h")
	boolFlag(fs, &ver, "version", "v")
	fs.IntVar(&jobs, "jobs", 0, "")
	fs.IntVar(&jobs, "j", 0, "")

	positional, err := parseArgs(fs, os.Args[1:])
	if err != nil {
		ui.Init(c.noColor)
		fail(strings.TrimPrefix(err.Error(), "flag provided but not defined: ") + "  (try quarry --help)")
	}
	if help || (len(positional) == 1 && positional[0] == "help") {
		printHelp()
		ui.Pause()
		return 0
	}
	ui.Init(c.noColor)
	if ver {
		fmt.Println("quarry " + version)
		return 0
	}
	if len(positional) > 1 {
		fail("too many arguments (try quarry --help)")
	}

	game, sources := c.resolve()
	cfg, _, _ := loadConfig(c.config)

	dest := ""
	bannerShown := false
	switch {
	case len(positional) == 1:
		dest = steam.HostPath(positional[0])
	case cfg.Destination != "":
		dest = steam.HostPath(cfg.Destination)
	case ui.OwnConsole() && len(os.Args) == 1:
		// double clicked with no args: ask instead of silently extracting 30gb
		ui.Banner(version)
		bannerShown = true
		dest = steam.HostPath(ui.Ask("Extract to:", defaultDest()))
		fmt.Println()
	default:
		dest = defaultDest()
	}

	if list {
		for _, s := range sources {
			fmt.Println(s)
		}
		return 0
	}

	opts := engine.Options{
		Dest: dest, Sources: sources, Include: splitList(include), Exclude: splitList(exclude),
		Jobs: jobs, Raw: raw, Force: force,
	}
	workers := jobs
	if workers <= 0 {
		workers = min(2*runtime.NumCPU(), 32)
	}
	if !quiet {
		if !bannerShown {
			ui.Banner(version)
		}
		ui.Field("game", game)
		ui.Field("dest", dest)
		ui.Field("pbos", fmt.Sprintf("%d  %s", len(sources), ui.Dim(fmt.Sprintf("(%d workers)", workers))))
		fmt.Println()
	}

	notice := make(chan string, 1)
	if !quiet && ui.TTY {
		go func() { notice <- update.Passive(version) }()
	}

	var stats engine.Stats
	start := time.Now()
	live := &ui.Live{}
	done := make(chan struct{})
	finished := make(chan struct{})
	if !quiet {
		live.Start()
		go func() {
			progress(&stats, start, live, done)
			close(finished)
		}()
	} else {
		close(finished)
	}

	// put the cursor back if someone hits ctrl+c mid run
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		live.Stop(false)
		fmt.Println("\n  " + ui.Bad("cancelled"))
		ui.Exit(130)
	}()

	res := engine.Run(opts, &stats)
	close(done)
	<-finished
	if !quiet {
		live.Stop(true)
		summary(&stats, &res.Changes, time.Since(start), len(res.Errors))
		if ui.TTY {
			updateNotice(notice)
		}
	}
	defer ui.Pause()
	if len(res.Errors) > 0 {
		sort.Slice(res.Errors, func(i, j int) bool { return res.Errors[i].Error() < res.Errors[j].Error() })
		fmt.Fprintf(os.Stderr, "  %s\n", ui.Bad(fmt.Sprintf("%d error(s)", len(res.Errors))))
		for i, e := range res.Errors {
			if i == 20 {
				fmt.Fprintf(os.Stderr, "    ... and %d more\n", len(res.Errors)-20)
				break
			}
			fmt.Fprintln(os.Stderr, "    "+e.Error())
		}
		return 1
	}
	return 0
}

func progress(s *engine.Stats, start time.Time, live *ui.Live, done <-chan struct{}) {
	interval := 80 * time.Millisecond
	if !ui.TTY {
		interval = 3 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			cur, _ := s.Current.Load().(string)
			total, proc := s.TotalBytes.Load(), s.Processed.Load()
			frac := 0.0
			if total > 0 {
				frac = float64(proc) / float64(total)
			}
			el := time.Since(start)
			written := s.Bytes.Load()
			speed := int64(float64(written) / el.Seconds())
			eta := "--"
			if frac > 0.02 && frac < 1 {
				eta = ui.Duration(time.Duration(float64(el) * (1 - frac) / frac))
			}
			dot := ui.Dim(" " + sym("·", "|") + " ")
			l1 := fmt.Sprintf("  %s %s  %s", ui.Bar(frac, 30), ui.Bold(fmt.Sprintf("%3d%%", int(frac*100))),
				ui.Dim(fmt.Sprintf("%d/%d PBOs", s.Done.Load(), s.Total.Load())))
			l2 := fmt.Sprintf("  %s files%s%s%s%s/s%sETA %s", ui.Commas(s.Files.Load()), dot, ui.Human(written), dot, ui.Human(speed), dot, eta)
			if !ui.TTY {
				fmt.Printf("%3d%%  %s files  %s\n", int(frac*100), ui.Commas(s.Files.Load()), ui.Human(written))
				continue
			}
			live.Draw(l1, l2, "  "+ui.Dim(sym("▸", ">")+" "+cur))
		}
	}
}

func sym(fancy, plain string) string {
	if ui.Unicode {
		return fancy
	}
	return plain
}

func summary(s *engine.Stats, ch *engine.Changeset, el time.Duration, errs int) {
	files, written := s.Files.Load(), s.Bytes.Load()
	if files == 0 && errs == 0 {
		fmt.Printf("  %s %s %s\n\n", ui.Good(sym("✔", "ok")), ui.Bold("Already up to date"),
			ui.Dim(fmt.Sprintf("(%d PBOs unchanged, checked in %s)", s.Skipped.Load(), ui.Duration(el))))
		return
	}
	mark := ui.Good(sym("✔", "ok")) + " " + ui.Bold("Done in "+ui.Duration(el))
	if errs > 0 {
		mark = ui.Bad(sym("✖", "!!")) + " " + ui.Bold("Finished with errors in "+ui.Duration(el))
	}
	fmt.Printf("  %s\n\n", mark)
	ui.Field("files", ui.Commas(files))
	ui.Field("size", ui.Human(written))
	ui.Field("speed", ui.Human(int64(float64(written)/el.Seconds()))+"/s")
	ui.Field("pbos", fmt.Sprintf("%d extracted, %d unchanged", s.Total.Load()-s.Skipped.Load(), s.Skipped.Load()))
	if ch.Total() > 0 && ch.AddedN != int(files) {
		ui.Field("diff", fmt.Sprintf("%s  %s  %s  %s", ui.Good(fmt.Sprintf("+%s", ui.Commas(int64(ch.AddedN)))),
			ui.Accent(fmt.Sprintf("~%s", ui.Commas(int64(ch.ModifiedN)))), ui.Bad(fmt.Sprintf("-%s", ui.Commas(int64(ch.RemovedN)))),
			ui.Dim("(quarry diff for the list)")))
	}

	top := s.TopExts(6)
	if len(top) > 0 {
		fmt.Printf("\n  %s\n", ui.Dim("top file types"))
		max := top[0].Files.Load()
		for _, e := range top {
			n := e.Files.Load()
			fmt.Printf("  %s %s  %s  %s\n", ui.Cyan(fmt.Sprintf("%-9s", e.Ext)), ui.Bar(float64(n)/float64(max), 12),
				fmt.Sprintf("%8s", ui.Commas(n)), ui.Dim(ui.Human(e.Bytes.Load())))
		}
	}
	fmt.Println()
}

func numCPU() int { return runtime.NumCPU() }
