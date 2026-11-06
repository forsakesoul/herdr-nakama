//go:build darwin

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// 读 termios 成功才是终端；/dev/null、/dev/zero 之类字符设备会失败。
func realStdinIsTTY() bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)))
	return e == 0
}
