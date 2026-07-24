// Package hardware detects the host accelerator and recommends a Sovereign
// runtime profile (design.md §18.2). Control runs inside a Linux container,
// so detection reads device files rather than shelling out; the install
// script sets SOVEREIGN_HOST_OS/SOVEREIGN_HOST_ARCH for facts the container
// cannot observe (macOS hosts).
package hardware

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

type Detection struct {
	Profile string   `json:"profile"`
	Reasons []string `json:"reasons"`
}

type GPU struct {
	Name      string `json:"name,omitempty"`
	VRAMBytes int64  `json:"vram_bytes,omitempty"`
}

// Inventory contains the capacity facts used by the recommendation UI. The
// host installer supplies facts a Linux container cannot observe accurately;
// container-visible values remain useful fallbacks for development installs.
type Inventory struct {
	OS               string      `json:"os"`
	Architecture     string      `json:"architecture"`
	Profile          string      `json:"profile"`
	MemoryBytes      int64       `json:"memory_bytes,omitempty"`
	StorageFreeBytes int64       `json:"storage_free_bytes,omitempty"`
	GPU              *GPU        `json:"gpu,omitempty"`
	Detections       []Detection `json:"detections"`
}

// probes are variables so tests can fake the filesystem.
var (
	pathExists = func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
	getenv   = os.Getenv
	goarch   = func() string { return runtime.GOARCH }
	readFile = os.Open
)

func envInt64(name string) int64 {
	value, _ := strconv.ParseInt(getenv(name), 10, 64)
	return value
}

func linuxMemoryBytes() int64 {
	file, err := readFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			value, _ := strconv.ParseInt(fields[1], 10, 64)
			return value * 1024
		}
	}
	return 0
}

func storageFreeBytes(path string) int64 {
	var stat syscall.Statfs_t
	if syscall.Statfs(path, &stat) != nil {
		return 0
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}

func GetInventory() Inventory {
	detections := Detect()
	profile := getenv("SOVEREIGN_PROFILE")
	if profile == "" && len(detections) > 0 {
		profile = detections[0].Profile
	}
	hostOS := getenv("SOVEREIGN_HOST_OS")
	if hostOS == "" {
		hostOS = runtime.GOOS
	}
	arch := getenv("SOVEREIGN_HOST_ARCH")
	if arch == "" {
		arch = goarch()
	}
	memory := envInt64("SOVEREIGN_HOST_MEMORY_BYTES")
	if memory == 0 {
		memory = linuxMemoryBytes()
	}
	inventory := Inventory{
		OS: hostOS, Architecture: arch, Profile: profile, MemoryBytes: memory,
		StorageFreeBytes: storageFreeBytes(getenv("SOVEREIGN_ROOT")), Detections: detections,
	}
	if inventory.StorageFreeBytes == 0 {
		inventory.StorageFreeBytes = storageFreeBytes("/")
	}
	name := getenv("SOVEREIGN_GPU_NAME")
	vramMiB := envInt64("SOVEREIGN_GPU_VRAM_MIB")
	if name != "" || vramMiB > 0 {
		inventory.GPU = &GPU{Name: name, VRAMBytes: vramMiB * 1024 * 1024}
	}
	return inventory
}

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

	// /dev/accel is the Gaudi (habana accel driver) node, not an Intel GPU.
	if pathExists("/dev/accel") || pathExists("/sys/class/accel") {
		detections = append(detections, Detection{
			Profile: "gaudi-x86_64",
			Reasons: []string{"/dev/accel present (Intel Gaudi accelerator)"},
		})
	}

	if pathExists("/dev/dri") && pathExists("/sys/module/intel_gpu_top") {
		detections = append(detections, Detection{
			Profile: "xpu-x86_64",
			Reasons: []string{"Intel GPU device files present"},
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
