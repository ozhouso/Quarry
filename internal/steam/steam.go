// Package steam - finds where the game is installed by poking around steam's files
package steam

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var (
	driveRe = regexp.MustCompile(`^([A-Za-z]):[\\/](.*)$`)
	pathRe  = regexp.MustCompile(`"path"\s+"([^"]+)"`)
)

// HostPath swaps a windows style path (C:\...) for a /mnt one when running under wsl
func HostPath(p string) string {
	if runtime.GOOS != "linux" {
		return filepath.FromSlash(p)
	}
	if m := driveRe.FindStringSubmatch(p); m != nil {
		return "/mnt/" + strings.ToLower(m[1]) + "/" + strings.ReplaceAll(m[2], `\`, "/")
	}
	return p
}

func steamRoots() []string {
	var roots []string
	if v := os.Getenv("STEAM_PATH"); v != "" {
		roots = append(roots, HostPath(v))
	}
	if out, err := exec.Command("reg.exe", "query", `HKCU\Software\Valve\Steam`, "/v", "SteamPath").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if i := strings.Index(line, "REG_SZ"); i >= 0 {
				roots = append(roots, HostPath(strings.TrimSpace(line[i+6:])))
			}
		}
	}
	roots = append(roots,
		HostPath(`C:\Program Files (x86)\Steam`),
		HostPath(`C:\Program Files\Steam`),
	)
	return roots
}

func libraries(root string) []string {
	libs := []string{root}
	for _, rel := range []string{filepath.Join("config", "libraryfolders.vdf"), filepath.Join("steamapps", "libraryfolders.vdf")} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for _, m := range pathRe.FindAllStringSubmatch(string(data), -1) {
			libs = append(libs, HostPath(strings.ReplaceAll(m[1], `\\`, `\`)))
		}
		break
	}
	return libs
}

// FindDayZ looks for the game folder, stable or experimental depending on the flag
func FindDayZ(experimental bool) (string, bool) {
	dir := "DayZ"
	if experimental {
		dir = "DayZ Exp"
	}
	for _, root := range steamRoots() {
		for _, lib := range libraries(root) {
			p := filepath.Join(lib, "steamapps", "common", dir)
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p, true
			}
		}
	}
	return "", false
}
