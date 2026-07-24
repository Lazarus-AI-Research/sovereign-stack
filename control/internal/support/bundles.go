package support

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/jobs"
)

type Deps struct {
	Root    string
	Version string
	Profile string
}

type Bundle struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
}

var safeEnvironmentValues = map[string]bool{
	"SOVEREIGN_VERSION": true, "SOVEREIGN_PROFILE": true, "SOVEREIGN_ACCESS_MODE": true,
	"SOVEREIGN_BIND_ADDRESS": true, "SOVEREIGN_SITE_ADDRESS": true, "SOVEREIGN_PUBLIC_URL": true,
	"SOVEREIGN_HOST_OS": true, "SOVEREIGN_HOST_ARCH": true, "SOVEREIGN_HOST_MEMORY_BYTES": true,
	"SOVEREIGN_GPU_NAME": true, "SOVEREIGN_GPU_VRAM_MIB": true, "HTTP_PORT": true, "HTTPS_PORT": true,
}

func redactedEnvironment(raw []byte) []byte {
	lines := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if !safeEnvironmentValues[key] {
			value = "<redacted>"
		}
		lines = append(lines, key+"="+value)
	}
	sort.Strings(lines)
	return []byte(strings.Join(lines, "\n") + "\n")
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func archiveDirectory(source, destination string) error {
	file, err := os.OpenFile(destination+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	err = filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		name, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if closeErr := tarWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gzipWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(destination + ".tmp")
		return err
	}
	return os.Rename(destination+".tmp", destination)
}

func checksum(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	bytes, err := io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), bytes, err
}

func (d *Deps) Handle(ctx context.Context, _ json.RawMessage) (any, error) {
	total := int64(3)
	_ = jobs.Report(ctx, jobs.Progress{Stage: "collecting", Message: "Collecting appliance metadata", Current: 0, Total: &total, Unit: "steps"})
	id := time.Now().UTC().Format("20060102-150405")
	root := filepath.Join(d.Root, "support")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(root, ".support-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	metadata, _ := json.MarshalIndent(map[string]any{
		"created_at": time.Now().UTC().Format(time.RFC3339), "version": d.Version, "profile": d.Profile,
		"privacy": "Secrets and content logs are excluded; environment values are allowlist-redacted.",
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(stage, "system.json"), append(metadata, '\n'), 0o600); err != nil {
		return nil, err
	}
	_ = jobs.Report(ctx, jobs.Progress{Stage: "redacting", Message: "Redacting configuration", Current: 1, Total: &total, Unit: "steps"})
	if environment, err := os.ReadFile(filepath.Join(d.Root, ".env")); err == nil {
		_ = os.WriteFile(filepath.Join(stage, "environment.txt"), redactedEnvironment(environment), 0o600)
	}
	configRoot := filepath.Join(stage, "config")
	_ = os.MkdirAll(configRoot, 0o700)
	for _, name := range []string{"branding.yaml", "feature-flags.yaml", "runtime.yaml", "model-registry.yaml", "embedding-profiles.yaml"} {
		source := filepath.Join(d.Root, "config", name)
		if _, err := os.Stat(source); err == nil {
			if err := copyFile(source, filepath.Join(configRoot, name)); err != nil {
				return nil, err
			}
		}
	}
	// The host lifecycle audit contains only allowlisted operation names,
	// outcomes, and non-secret version/mode details. Include it so support can
	// reconstruct repair/update failures without collecting prompts or logs.
	hostOperations := filepath.Join(d.Root, "logs", "hostd", "operations.jsonl")
	if _, err := os.Stat(hostOperations); err == nil {
		if err := copyFile(hostOperations, filepath.Join(stage, "host-operations.jsonl")); err != nil {
			return nil, err
		}
	}
	_ = jobs.Report(ctx, jobs.Progress{Stage: "packaging", Message: "Packaging redacted diagnostics", Current: 2, Total: &total, Unit: "steps"})
	archive := filepath.Join(root, "sovereign-support-"+id+".tar.gz")
	if err := archiveDirectory(stage, archive); err != nil {
		return nil, fmt.Errorf("support archive: %w", err)
	}
	hash, bytes, err := checksum(archive)
	if err != nil {
		return nil, err
	}
	_ = jobs.Report(ctx, jobs.Progress{Stage: "complete", Message: "Support bundle ready", Current: 3, Total: &total, Unit: "steps"})
	return Bundle{ID: id, CreatedAt: time.Now().UTC().Format(time.RFC3339), Bytes: bytes, SHA256: hash}, nil
}

func (d *Deps) List() ([]Bundle, error) {
	entries, err := os.ReadDir(filepath.Join(d.Root, "support"))
	if os.IsNotExist(err) {
		return []Bundle{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := []Bundle{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "sovereign-support-") || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}
		path := filepath.Join(d.Root, "support", entry.Name())
		hash, bytes, err := checksum(path)
		if err != nil {
			continue
		}
		info, _ := entry.Info()
		id := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "sovereign-support-"), ".tar.gz")
		items = append(items, Bundle{ID: id, CreatedAt: info.ModTime().UTC().Format(time.RFC3339), Bytes: bytes, SHA256: hash})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items, nil
}

func (d *Deps) Path(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, "/\\.") {
		return "", fmt.Errorf("invalid support bundle id")
	}
	path := filepath.Join(d.Root, "support", "sovereign-support-"+id+".tar.gz")
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}
