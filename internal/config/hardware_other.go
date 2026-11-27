//go:build !windows && !linux && !darwin

package config

func detectRAMBytes() uint64 {
	return 0
}
