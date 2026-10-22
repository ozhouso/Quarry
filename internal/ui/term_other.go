//go:build !windows

package ui

import (
	"os"
	"strings"
)

func enableVT() bool { return true }

func unicodeOK() bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(k); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}

func OwnConsole() bool { return false }
