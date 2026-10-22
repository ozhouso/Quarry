// Package ui is all the terminal prettiness: colours, the banner, the progress bar.
// No dependencies, just ansi escapes.
package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

var (
	Color   bool // are we allowed to use colours
	Unicode bool // can we draw the fancy bar characters
	TTY     bool // stdout is a real terminal
)

// Init works out what the terminal can do. Call it once at startup.
func Init(noColor bool) {
	if st, err := os.Stdout.Stat(); err == nil {
		TTY = st.Mode()&os.ModeCharDevice != 0
	}
	vt := enableVT() // only does something on windows
	Color = TTY && vt && !noColor && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	Unicode = TTY && unicodeOK()
}

func paint(code, s string) string {
	if !Color {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func Accent(s string) string { return paint("38;5;214", s) }
func Bold(s string) string   { return paint("1", s) }
func Dim(s string) string    { return paint("38;5;245", s) }
func Good(s string) string   { return paint("38;5;78", s) }
func Bad(s string) string    { return paint("38;5;203", s) }
func Cyan(s string) string   { return paint("38;5;80", s) }

func sym(fancy, plain string) string {
	if Unicode {
		return fancy
	}
	return plain
}

// Banner prints the logo block
func Banner(version string) {
	fmt.Println()
	fmt.Printf("  %s %s  %s\n", Accent(sym("◆", "*")), Bold(Accent("QUARRY")), Dim("v"+version))
	fmt.Printf("  %s\n\n", Dim("fast DayZ PBO extractor"))
}

// Field prints an aligned "label  value" row
func Field(label, value string) {
	fmt.Printf("  %s  %s\n", Dim(fmt.Sprintf("%-6s", label)), value)
}

// Bar draws a progress bar of the given width for a 0..1 fraction
func Bar(frac float64, width int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	full := int(frac*float64(width) + 0.5)
	if Unicode {
		return Accent(strings.Repeat("▰", full)) + Dim(strings.Repeat("▱", width-full))
	}
	return "[" + strings.Repeat("#", full) + strings.Repeat("-", width-full) + "]"
}

// Live redraws a few lines in place, so the bar doesnt scroll the screen
type Live struct{ lines int }

func (l *Live) Start() {
	if TTY && Color {
		fmt.Print("\033[?25l") // hide cursor
	}
}

func (l *Live) Draw(lines ...string) {
	if l.lines > 0 {
		fmt.Printf("\033[%dA", l.lines)
	}
	for _, ln := range lines {
		fmt.Printf("\r\033[K%s\n", ln)
	}
	l.lines = len(lines)
}

// Stop wipes the live area when clear is true, otherwise leaves the last frame there
func (l *Live) Stop(clear bool) {
	if clear && l.lines > 0 {
		fmt.Printf("\033[%dA", l.lines)
		for i := 0; i < l.lines; i++ {
			fmt.Print("\r\033[K\n")
		}
		fmt.Printf("\033[%dA", l.lines)
	}
	l.lines = 0
	if TTY && Color {
		fmt.Print("\033[?25h")
	}
}

func Human(n int64) string {
	f := float64(n)
	for _, u := range []string{"B", "KB", "MB", "GB"} {
		if f < 1024 {
			if u == "B" {
				return fmt.Sprintf("%d %s", n, u)
			}
			return fmt.Sprintf("%.1f %s", f, u)
		}
		f /= 1024
	}
	return fmt.Sprintf("%.2f TB", f)
}

func Duration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

// Commas puts thousands separators in, 135354 -> 135,354
func Commas(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Pause keeps the window open when we were double clicked, otherwise it vanishes
// before anyone can read the output
func Pause() {
	if !OwnConsole() {
		return
	}
	fmt.Print("\n  " + Dim("Press Enter to close..."))
	bufio.NewReader(os.Stdin).ReadString('\n')
}

// Ask prints a prompt and returns what was typed, or def if they just hit enter
func Ask(prompt, def string) string {
	fmt.Printf("  %s %s ", Bold(prompt), Dim("["+def+"]"))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if line = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), `"`)); line == "" {
		return def
	}
	return line
}

// Exit pauses (if needed) and then quits
func Exit(code int) {
	Pause()
	os.Exit(code)
}
