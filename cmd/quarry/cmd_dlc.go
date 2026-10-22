package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"quarry/internal/ui"
)

const dlcHelp = `usage: quarry dlc

  shows the DLC / map folders found in your DayZ install and whether they
  are switched on by quarry.json or --dlc.

  quarry init          writes a starter quarry.json in the current folder
  --dlc nasdara,sakhal on extract / ls / cat / grep: only use these DLC
  --dlc none           base game only
`

func cmdDLC(args []string) int {
	fs := newFlags("dlc")
	var c common
	var help bool
	c.bind(fs)
	boolFlag(fs, &help, "help", "h")
	_, err := parseArgs(fs, args)
	ui.Init(c.noColor)
	if err != nil || help {
		fmt.Print(dlcHelp)
		return 0
	}
	game := c.gameDir()
	found := detectDLC(game)
	enabled, set := c.dlcSelection()

	fmt.Println()
	ui.Field("game", game)
	if p, ok := findConfig(c.config); ok {
		ui.Field("config", p)
	}
	fmt.Println()
	if len(found) == 0 {
		fmt.Println("  " + ui.Dim("no DLC folders found"))
		return 0
	}
	for _, f := range found {
		on := !set
		for _, w := range enabled {
			on = on || strings.EqualFold(w, f.Name)
		}
		mark := ui.Good(sym("●", "[x]"))
		if !on {
			mark = ui.Dim(sym("○", "[ ]"))
		}
		fmt.Printf("  %s %-14s %s\n", mark, f.Name, ui.Dim(fmt.Sprintf("%d PBOs", f.PBOs)))
	}
	fmt.Println()
	return 0
}

func cmdInit(args []string) int {
	fs := newFlags("init")
	var c common
	var force bool
	c.bind(fs)
	boolFlag(fs, &force, "force", "")
	_, _ = parseArgs(fs, args)
	ui.Init(c.noColor)
	if _, err := os.Stat(configName); err == nil && !force {
		fail(configName + " already exists (use --force to overwrite)")
	}
	var names []string
	for _, f := range detectDLC(c.gameDir()) {
		names = append(names, f.Name)
	}
	cfg := config{DLC: &names, Mods: []string{}}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(configName, append(data, '\n'), 0o644); err != nil {
		fail(err.Error())
	}
	fmt.Printf("  %s wrote %s\n", ui.Good(sym("✔", "ok")), configName)
	fmt.Println("  " + ui.Dim("remove a name from \"dlc\" to skip it, add workshop mods under \"mods\""))
	return 0
}
