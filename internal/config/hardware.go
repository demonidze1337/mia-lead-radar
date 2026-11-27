package config

import (
	"math"
	"runtime"

	"github.com/demonidze1337/mia-lead-radar/internal/domain"
)

const bytesPerGiB = uint64(1) << 30

// DetectHardware inspects host RAM, CPU count, and platform to recommend an
// LLM hardware tier. Unknown RAM safely falls back to the balanced tier.
func DetectHardware() domain.HardwareProfile {
	return buildHardwareProfile(detectRAMBytes(), runtime.NumCPU(), runtime.GOOS, runtime.GOARCH)
}

// TierForRAM maps total memory to a hardware tier:
// <12 GiB weak, 12-23 GiB balanced, >=24 GiB workstation.
func TierForRAM(ramBytes uint64) domain.HardwareTier {
	switch {
	case ramBytes == 0:
		return domain.HardwareTierBalanced
	case ramBytes < 12*bytesPerGiB:
		return domain.HardwareTierWeak
	case ramBytes < 24*bytesPerGiB:
		return domain.HardwareTierBalanced
	default:
		return domain.HardwareTierWorkstation
	}
}

func buildHardwareProfile(ramBytes uint64, cpus int, goos, goarch string) domain.HardwareProfile {
	if cpus < 1 {
		cpus = 1
	}
	ramGB := float64(ramBytes) / float64(bytesPerGiB)
	return domain.HardwareProfile{
		RAMBytes:        ramBytes,
		RAMGB:           math.Round(ramGB*100) / 100,
		CPUCores:        cpus,
		OS:              goos,
		Arch:            goarch,
		RecommendedTier: TierForRAM(ramBytes),
	}
}
