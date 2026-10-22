//go:build windows

package logx

import (
	"os"

	"golang.org/x/sys/windows"
)

func enableWindowsANSI() {
	enableFD(windows.Stdout)
	enableFD(windows.Stderr)
	_ = os.Stdout
}

func enableFD(fd windows.Handle) {
	var mode uint32
	if err := windows.GetConsoleMode(fd, &mode); err != nil {
		return
	}
	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	_ = windows.SetConsoleMode(fd, mode)
}
