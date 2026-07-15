package bundles

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestArchivePathsPreservesRelativeLayout(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "models", "one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "models", "one", "weights.bin"), []byte("weights"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "models.tar.gz")
	if err := archivePaths(destination, root, []string{"models"}); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		header, err := tr.Next()
		if err != nil {
			break
		}
		if header.Name == "models/one/weights.bin" {
			found = true
		}
	}
	if !found {
		t.Fatal("archive did not preserve the model path")
	}
}

func TestModelPathsAreProfileSpecific(t *testing.T) {
	metal, err := modelPaths("metal-arm64", []string{"assistant-large", "embedding-text-compact"})
	if err != nil || len(metal) != 3 {
		t.Fatalf("metal paths = %v, %v", metal, err)
	}
	if _, err := modelPaths("metal-arm64", []string{"embedding-omni-default"}); err == nil {
		t.Fatal("omni model should be rejected on Metal")
	}
	all, err := modelPaths("cuda-x86_64", []string{"all", "assistant-large"})
	if err != nil || len(all) != 1 || all[0] != "." {
		t.Fatalf("all paths = %v, %v", all, err)
	}
}
