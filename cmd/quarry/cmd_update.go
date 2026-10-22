package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"quarry/internal/ui"
	"quarry/internal/update"
)

const updateHelp = `usage: quarry update [--check]

  downloads the newest release from GitHub and replaces this exe with it.
  The download is checked against the release's checksums.txt first.

      --check   only say whether a newer version exists

  no login is needed once the repo is public. While it's private quarry looks for a
  token in QUARRY_TOKEN, GH_TOKEN, GITHUB_TOKEN, or the GitHub CLI (gh auth login).
  set QUARRY_NO_UPDATE_CHECK=1 to turn off the daily "update available" note.
`

func cmdUpdate(args []string) int {
	fs := newFlags("update")
	var check, help, noColor bool
	boolFlag(fs, &check, "check", "")
	boolFlag(fs, &help, "help", "h")
	boolFlag(fs, &noColor, "no-color", "")
	_, err := parseArgs(fs, args)
	ui.Init(noColor)
	if err != nil || help {
		fmt.Print(updateHelp)
		return 0
	}

	token := update.Token()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	rel, err := update.Latest(ctx, token)
	if err != nil {
		fail(err.Error())
	}
	if !update.Newer(rel.Tag, version) {
		fmt.Printf("  %s you're on the latest version %s\n", ui.Good(sym("✔", "ok")), ui.Dim("("+version+")"))
		return 0
	}
	fmt.Printf("  %s %s  %s\n", ui.Accent("update available"), ui.Bold(rel.Tag), ui.Dim("(you have "+version+")"))
	if notes := strings.TrimSpace(rel.Body); notes != "" {
		for i, ln := range strings.Split(notes, "\n") {
			if i == 12 {
				fmt.Println("    " + ui.Dim("..."))
				break
			}
			fmt.Println("    " + ui.Dim(ln))
		}
	}
	if check {
		fmt.Println("\n  run " + ui.Bold("quarry update") + " to install it")
		return 0
	}

	fmt.Println()
	err = update.Apply(ctx, rel, token, func(done, total int64) {
		if total > 0 && ui.TTY {
			fmt.Printf("\r\033[K  %s %3d%%", ui.Bar(float64(done)/float64(total), 30), done*100/total)
		}
	})
	if ui.TTY {
		fmt.Print("\r\033[K")
	}
	if err != nil {
		fail(err.Error())
	}
	fmt.Printf("  %s updated to %s\n", ui.Good(sym("✔", "ok")), ui.Bold(rel.Tag))
	return 0
}

// quiet one-line nudge shown after a normal run, only on a real terminal
func updateNotice(ch <-chan string) {
	select {
	case tag := <-ch:
		if tag != "" {
			fmt.Printf("  %s %s is out, run %s\n\n", ui.Accent(sym("↑", "^")), ui.Bold(tag), ui.Bold("quarry update"))
		}
	case <-time.After(300 * time.Millisecond):
	}
}
