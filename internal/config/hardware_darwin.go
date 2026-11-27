//go:build darwin

package config

import "golang.org/x/sys/unix"

func detectRAMBytes() uint64 {
	ram, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return ram
}
