package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// quarry.json - everything is optional.
//
//	{
//	  "dlc":         ["nasdara", "sakhal"],
//	  "mods":        ["@CF"],
//	  "destination": "P:\\"
//	}
//
// if "dlc" is left out every dlc folder that's installed gets used. [] means base game only
type config struct {
	DLC         *[]string `json:"dlc,omitempty"`
	Mods        []string  `json:"mods,omitempty"`
	Destination string    `json:"destination,omitempty"`
}

const configName = "quarry.json"

// looks in the current folder first, then next to the exe
func findConfig(explicit string) (string, bool) {
	if explicit != "" {
		return explicit, true
	}
	cands := []string{configName}
	if exe, err := os.Executable(); err == nil {
		cands = append(cands, filepath.Join(filepath.Dir(exe), configName))
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, true
		}
	}
	return "", false
}

func loadConfig(explicit string) (config, string, error) {
	var cfg config
	p, ok := findConfig(explicit)
	if !ok {
		return cfg, "", nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return cfg, p, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, p, errors.New(p + ": " + err.Error())
	}
	return cfg, p, nil
}

type dlcInfo struct {
	Name string // folder name as it is on disk
	Dir  string // the addons folder
	PBOs int
}

// anything at the top of the game folder that has an addons dir with pbos in it
func detectDLC(game string) []dlcInfo {
	var out []dlcInfo
	top, _ := os.ReadDir(game)
	for _, e := range top {
		if !e.IsDir() {
			continue
		}
		low := strings.ToLower(e.Name())
		if low == "addons" || low == "dta" || strings.HasPrefix(e.Name(), "!") {
			continue
		}
		dir := ciChild(filepath.Join(game, e.Name()), "addons")
		if dir == "" {
			continue
		}
		if n := len(pboFiles(dir)); n > 0 {
			out = append(out, dlcInfo{Name: e.Name(), Dir: dir, PBOs: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// finds a child folder ignoring case, linux installs can have Addons or addons
func ciChild(parent, name string) string {
	ents, _ := os.ReadDir(parent)
	for _, e := range ents {
		if e.IsDir() && strings.EqualFold(e.Name(), name) {
			return filepath.Join(parent, e.Name())
		}
	}
	return ""
}

func pboFiles(dir string) []string {
	var out []string
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".pbo") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// collect builds the list of pbos: base game, the dlc we want, then any mods.
// dlc nil = use every dlc found, non-nil = only those names (empty means none)
func collect(game string, mods []string, dlc []string, dlcSet bool) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, d := range []string{"addons", "dta"} {
		if dir := ciChild(game, d); dir != "" {
			for _, p := range pboFiles(dir) {
				add(p)
			}
		}
	}

	found := detectDLC(game)
	for _, f := range found {
		use := !dlcSet
		for _, want := range dlc {
			if strings.EqualFold(want, f.Name) {
				use = true
			}
		}
		if use {
			for _, p := range pboFiles(f.Dir) {
				add(p)
			}
		}
	}
	for _, want := range dlc {
		ok := false
		for _, f := range found {
			ok = ok || strings.EqualFold(want, f.Name)
		}
		if !ok {
			return nil, errors.New("no DLC folder called " + want + "  (quarry dlc lists what's installed)")
		}
	}

	for _, m := range mods {
		m = hostPath(m)
		cands := []string{m, filepath.Join(game, m)}
		if strings.HasPrefix(m, "@") {
			cands = append(cands, filepath.Join(game, "!Workshop", m))
		}
		got := false
		for _, c := range cands {
			st, err := os.Stat(c)
			if err != nil {
				continue
			}
			got = true
			if st.IsDir() {
				dir := ciChild(c, "addons")
				if dir == "" {
					dir = c
				}
				for _, p := range pboFiles(dir) {
					add(p)
				}
			} else {
				add(c)
			}
			break
		}
		if !got {
			return nil, errors.New("mod not found: " + m)
		}
	}
	sort.Strings(out)
	return out, nil
}
