//go:build windows

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

// turns on ansi processing + utf8 output, older consoles have both off by default
func enableVT() bool {
	k := syscall.NewLazyDLL("kernel32.dll")
	getMode := k.NewProc("GetConsoleMode")
	setMode := k.NewProc("SetConsoleMode")
	setCP := k.NewProc("SetConsoleOutputCP")
	setCP.Call(65001)
	var mode uint32
	h := os.Stdout.Fd()
	if r, _, _ := getMode.Call(h, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return false
	}
	r, _, _ := setMode.Call(h, uintptr(mode|0x0004)) // ENABLE_VIRTUAL_TERMINAL_PROCESSING
	return r != 0
}

func unicodeOK() bool { return true }

// true when the exe was double clicked, so windows made a console just for us.
// from a normal terminal there are more processes attached to the console
func OwnConsole() bool {
	var pids [4]uint32
	r, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList").Call(
		uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return r > 0 && r <= 2
}
