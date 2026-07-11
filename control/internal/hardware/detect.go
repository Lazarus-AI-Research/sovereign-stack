// Package hardware detects the host accelerator and recommends a Sovereign
// runtime profile (design.md §18.2). Control runs inside a Linux container,
// so detection reads device files rather than shelling out; the install
// script sets SOVEREIGN_HOST_OS/SOVEREIGN_HOST_ARCH for facts the container
// cannot observe (macOS hosts).
package hardware

import (
	"os"
	"runtime"
)

type Detection struct {
	Profile string   `json:"profile"`
	Reasons []string `json:"reasons"`
}

// probes are variables so tests can fake the filesystem.
var (
	pathExists = func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
	getenv = os.Getenv
	goarch = func() string { return runtime.GOARCH }
)

// Detect returns profile recommendations, most preferred first.
func Detect() []Detection {
	if forced := getenv("SOVEREIGN_FORCE_PROFILE"); forced != "" {
		return []Detection{{Profile: forced, Reasons: []string{"forced by SOVEREIGN_FORCE_PROFILE"}}}
	}

	var detections []Detection

	if getenv("SOVEREIGN_HOST_OS") == "darwin" {
		return append(detections, Detection{
			Profile: "metal-arm64",
			Reasons: []string{"install script reported a macOS host"},
		})
	}

	if pathExists("/proc/driver/nvidia/version") || pathExists("/dev/nvidia0") || pathExists("/dev/nvidiactl") {
		profile := "cuda-x86_64"
		reasons := []string{"NVIDIA driver/device files present"}
		if goarch() == "arm64" {
			profile = "cuda-arm64-dgx-spark"
			reasons = append(reasons, "arm64 host with NVIDIA devices (DGX Spark class)")
		}
		detections = append(detections, Detection{Profile: profile, Reasons: reasons})
	}

	if pathExists("/dev/kfd") {
		detections = append(detections, Detection{
			Profile: "rocm-x86_64",
			Reasons: []string{"/dev/kfd present (ROCm-capable AMD GPU)"},
		})
	}

	if pathExists("/dev/dri") && pathExists("/sys/module/intel_gpu_top") || pathExists("/dev/accel") {
		detections = append(detections, Detection{
			Profile: "xpu-x86_64",
			Reasons: []string{"Intel GPU/accelerator device files present"},
		})
	}

	fallback := "cpu-x86_64"
	if goarch() == "arm64" {
		fallback = "cpu-arm64"
	}
	detections = append(detections, Detection{
		Profile: fallback,
		Reasons: []string{"CPU fallback always available"},
	})
	return detections
}
