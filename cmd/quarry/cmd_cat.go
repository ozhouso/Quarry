package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"quarry/internal/engine"
	"quarry/internal/pbo"
	"quarry/internal/ui"
)

const catHelp = `usage: quarry cat <file> [options]

  prints one file straight out of the PBOs, no extraction needed.
  <file> can be a full name, the end of a path, or a glob:

    quarry cat PlayerBase.c
    quarry cat 4_World/Entities/ManBase/PlayerBase.c
    quarry cat DZ/AI/config.cpp

  -p, --pbo NAME   only look inside this PBO
      --raw        don't convert binarized configs to text
      --all        print every match instead of stopping when there are several
  -g, -x, -m       same game options as extract
`

type match struct {
	a *pbo.Archive
	e *pbo.Entry
}

// slashes + lowercase so windows and linux style patterns both work
func norm(s string) string { return strings.ToLower(strings.ReplaceAll(s, `\`, "/")) }

func entryMatches(pat, full string) bool {
	if full == pat || strings.HasSuffix(full, "/"+pat) {
		return true
	}
	if strings.ContainsAny(pat, "*?[") {
		if ok, _ := path.Match(pat, full); ok {
			return true
		}
		// let the glob match the tail of the path as well
		for i := 0; i < len(full); i++ {
			if full[i] == '/' {
				if ok, _ := path.Match(pat, full[i+1:]); ok {
					return true
				}
			}
		}
	}
	return false
}

// config.bin is shown to people as config.cpp, so let them ask for either
func altName(full string) string {
	if strings.HasSuffix(full, ".cpp") {
		return strings.TrimSuffix(full, ".cpp") + ".bin"
	}
	return ""
}

func cmdCat(args []string) int {
	fs := newFlags("cat")
	var c common
	var pboName string
	var raw, all, help bool
	c.bind(fs)
	stringFlag(fs, &pboName, "pbo", "p")
	boolFlag(fs, &raw, "raw", "")
	boolFlag(fs, &all, "all", "")
	boolFlag(fs, &help, "help", "h")
	pos, err := parseArgs(fs, args)
	ui.Init(c.noColor)
	if err != nil || help || len(pos) != 1 {
		fmt.Print(catHelp)
		return 0
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

	pat := norm(pos[0])
	alt := altName(pat)
	var matches []match
	for _, a := range archives {
		for i := range a.Entries {
			full := norm(a.Prefix() + "/" + a.Entries[i].Name)
			if entryMatches(pat, full) || (alt != "" && entryMatches(alt, full)) {
				matches = append(matches, match{a, &a.Entries[i]})
			}
		}
	}
	if len(matches) == 0 {
		fail("nothing matches " + pos[0])
	}
	sort.Slice(matches, func(i, j int) bool {
		return gameName(matches[i].a, matches[i].e.Name) < gameName(matches[j].a, matches[j].e.Name)
	})
	if len(matches) > 1 && !all {
		fmt.Fprintf(os.Stderr, "%s %d files match, be more specific or use --all:\n", ui.Bad("error"), len(matches))
		for i, m := range matches {
			if i == 25 {
				fmt.Fprintf(os.Stderr, "  ... and %d more\n", len(matches)-25)
				break
			}
			fmt.Fprintln(os.Stderr, "  "+gameName(m.a, m.e.Name))
		}
		return 1
	}

	var h handles
	defer h.closeAll()
	scratch := make([]byte, 0, 1<<20)
	for _, m := range matches {
		f, err := h.get(m.a.Path)
		if err != nil {
			fail(err.Error())
		}
		data, _, err := engine.Load(f, m.e, raw, &scratch)
		if err != nil {
			fail(m.e.Name + ": " + err.Error())
		}
		if ui.TTY && bytes.IndexByte(data[:min(len(data), 4096)], 0) >= 0 {
			fmt.Fprintf(os.Stderr, "%s %s is binary (%s), redirect it to a file: quarry cat ... > file\n",
				ui.Bad("error"), gameName(m.a, m.e.Name), ui.Human(int64(len(data))))
			return 1
		}
		if all && len(matches) > 1 {
			fmt.Printf("%s\n", ui.Accent("==> "+gameName(m.a, m.e.Name)+" <=="))
		}
		os.Stdout.Write(data)
		if all && len(matches) > 1 {
			fmt.Println()
		}
	}
	return 0
}
