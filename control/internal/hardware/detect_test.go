package hardware

import "testing"

func fakeEnv(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func fakePaths(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return func(path string) bool { return set[path] }
}

func withProbes(t *testing.T, env map[string]string, arch string, paths ...string) {
	t.Helper()
	origEnv, origPaths, origArch := getenv, pathExists, goarch
	t.Cleanup(func() { getenv, pathExists, goarch = origEnv, origPaths, origArch })
	getenv = fakeEnv(env)
	pathExists = fakePaths(paths...)
	goarch = func() string { return arch }
}

func first(t *testing.T) Detection {
	t.Helper()
	detections := Detect()
	if len(detections) == 0 {
		t.Fatal("no detections")
	}
	return detections[0]
}

func TestForcedProfileWins(t *testing.T) {
	withProbes(t, map[string]string{"SOVEREIGN_FORCE_PROFILE": "rocm-strix-halo"}, "amd64", "/dev/nvidia0")
	if got := first(t).Profile; got != "rocm-strix-halo" {
		t.Errorf("forced profile: got %s", got)
	}
}

func TestDarwinHostRecommendsMetal(t *testing.T) {
	withProbes(t, map[string]string{"SOVEREIGN_HOST_OS": "darwin"}, "arm64")
	if got := first(t).Profile; got != "metal-arm64" {
		t.Errorf("darwin host: got %s", got)
	}
}

func TestNvidiaX86(t *testing.T) {
	withProbes(t, nil, "amd64", "/proc/driver/nvidia/version")
	if got := first(t).Profile; got != "cuda-x86_64" {
		t.Errorf("nvidia x86: got %s", got)
	}
}

func TestNvidiaArm64IsDGXSpark(t *testing.T) {
	withProbes(t, nil, "arm64", "/dev/nvidia0")
	if got := first(t).Profile; got != "cuda-arm64-dgx-spark" {
		t.Errorf("nvidia arm64: got %s", got)
	}
}

func TestRocm(t *testing.T) {
	withProbes(t, nil, "amd64", "/dev/kfd")
	if got := first(t).Profile; got != "rocm-x86_64" {
		t.Errorf("rocm: got %s", got)
	}
}

func TestCPUFallbackAlwaysPresent(t *testing.T) {
	withProbes(t, nil, "arm64")
	detections := Detect()
	last := detections[len(detections)-1]
	if last.Profile != "cpu-arm64" {
		t.Errorf("fallback: got %s", last.Profile)
	}
}
