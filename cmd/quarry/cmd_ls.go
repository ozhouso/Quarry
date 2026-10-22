package main

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"quarry/internal/ui"
)

const lsHelp = `usage: quarry ls [pbo...] [-i globs] [options]

  no pbo given   lists every PBO with its prefix, file count and size
  with a pbo     lists the files inside it (name, prefix or path all work)

  -i, --include LIST   only show files matching these globs
  -g, -x, -m           same game options as extract
`

func cmdLs(args []string) int {
	fs := newFlags("ls")
	var c common
	var include string
	var help bool
	c.bind(fs)
	stringFlag(fs, &include, "include", "i")
	boolFlag(fs, &help, "help", "h")
	pos, err := parseArgs(fs, args)
	ui.Init(c.noColor)
	if err != nil || help {
		fmt.Print(lsHelp)
		return 0
	}
	_, sources := c.resolve()

	if len(pos) == 0 {
		archives, bad := openAll(sources)
		warnErrs(bad)
		sort.Slice(archives, func(i, j int) bool { return archives[i].Path < archives[j].Path })
		fmt.Printf("%s  %s  %s  %s\n", ui.Dim(fmt.Sprintf("%-34s", "PBO")), ui.Dim(fmt.Sprintf("%-28s", "PREFIX")),
			ui.Dim(fmt.Sprintf("%8s", "FILES")), ui.Dim(fmt.Sprintf("%10s", "SIZE")))
		var files int
		var size int64
		for _, a := range archives {
			name := path.Base(strings.ReplaceAll(a.Path, `\`, "/"))
			fmt.Printf("%-34s  %-28s  %8s  %10s\n", name, ui.Dim(a.Prefix()), ui.Commas(int64(len(a.Entries))), ui.Human(a.Size))
			files += len(a.Entries)
			size += a.Size
		}
		fmt.Printf("\n%s\n", ui.Dim(fmt.Sprintf("%d PBOs, %s files, %s", len(archives), ui.Commas(int64(files)), ui.Human(size))))
		return 0
	}

	var paths []string
	for _, p := range pos {
		found := findPBOs(sources, p)
		if len(found) == 0 {
			fail("no PBO named " + p + "  (quarry ls shows them all)")
		}
		paths = append(paths, found...)
	}
	archives, bad := openAll(paths)
	warnErrs(bad)
	globs := splitList(include)
	for _, a := range archives {
		for _, e := range a.Entries {
			if len(globs) > 0 && !globMatch(globs, baseName(e.Name)) {
				continue
			}
			size := e.OrigSize
			if size == 0 {
				size = e.DataSize
			}
			fmt.Printf("%s  %s\n", fmt.Sprintf("%10s", ui.Human(int64(size))), gameName(a, e.Name))
		}
	}
	return 0
}

func baseName(n string) string {
	if i := strings.LastIndexAny(n, `\/`); i >= 0 {
		return n[i+1:]
	}
	return n
}

func globMatch(globs []string, name string) bool {
	name = strings.ToLower(name)
	for _, g := range globs {
		if ok, _ := path.Match(strings.ToLower(g), name); ok {
			return true
		}
	}
	return false
}
