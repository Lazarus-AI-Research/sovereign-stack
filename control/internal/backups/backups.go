// Package backups implements §2.5/§18.11: configuration + database backups
// with checksummed manifests. Weights and caches are never included. The
// database dumps run in a fixed container via the restricted proxy; the
// config tarball and manifest are written by Control through the /sovereign
// mount.
package backups

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
	"strings"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
)

type Deps struct {
	Proxy *dockerproxy.Client
	// Root is the deploy mount ("/sovereign"); backups live in Root/backups.
	Root string
	// Databases must match the proxy's configured backup database list.
	Databases []string
	// DumpTimeout bounds the wait for database dumps to appear.
	DumpTimeout time.Duration
}

type ManifestFile struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	ID        string         `json:"id"`
	CreatedAt string         `json:"created_at"`
	Files     []ManifestFile `json:"files"`
	Excludes  []string       `json:"excludes"`
}

func (d Deps) dir(id string) string { return filepath.Join(d.Root, "backups", id) }

// ── create ───────────────────────────────────────────────────────────────

func (d Deps) HandleBackup(ctx context.Context, _ json.RawMessage) (any, error) {
	id := time.Now().UTC().Format("20060102-150405")
	dir := d.dir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	if err := d.archiveConfig(dir); err != nil {
		return nil, fmt.Errorf("config archive: %w", err)
	}

	if _, err := d.Proxy.RunBackup(ctx, "dump", id); err != nil {
		return nil, fmt.Errorf("database dump: %w", err)
	}
	if err := d.waitForDumps(ctx, dir); err != nil {
		return nil, err
	}

	manifest, err := d.writeManifest(id, dir)
	if err != nil {
		return nil, err
	}
	return map[string]any{"backup_id": id, "files": len(manifest.Files)}, nil
}

// archiveConfig tars deploy config + branding. The deploy .env (secrets)
// lives outside these directories and is deliberately not included (§22).
func (d Deps) archiveConfig(dir string) error {
	target, err := os.Create(filepath.Join(dir, "config.tar.gz"))
	if err != nil {
		return err
	}
	defer target.Close()
	gz := gzip.NewWriter(target)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, sub := range []string{"config", "branding"} {
		root := filepath.Join(d.Root, sub)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			relative, err := filepath.Rel(d.Root, path)
			if err != nil {
				return err
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = relative
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(tw, file)
			return err
		})
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (d Deps) waitForDumps(ctx context.Context, dir string) error {
	timeout := d.DumpTimeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		missing := d.missingDumps(dir)
		if len(missing) == 0 {
			// give the last gzip a moment to flush
			time.Sleep(2 * time.Second)
			if len(d.missingDumps(dir)) == 0 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("database dumps incomplete after %s: missing %v", timeout, d.missingDumps(dir))
}

func (d Deps) missingDumps(dir string) []string {
	var missing []string
	for _, db := range d.Databases {
		path := filepath.Join(dir, db+".sql.gz")
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			missing = append(missing, db)
		}
	}
	return missing
}

func (d Deps) writeManifest(id, dir string) (*Manifest, error) {
	manifest := &Manifest{
		ID:        id,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Excludes:  []string{"model weights", "hf cache", "runtime compilation cache", "deploy .env"},
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "manifest.json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		sum, size, err := fileSHA256(path)
		if err != nil {
			return nil, err
		}
		manifest.Files = append(manifest.Files, ManifestFile{Name: entry.Name(), Bytes: size, SHA256: sum})
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return manifest, os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644)
}

func fileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

// ── list / verify / restore ──────────────────────────────────────────────

func (d Deps) List() ([]Manifest, error) {
	entries, err := os.ReadDir(filepath.Join(d.Root, "backups"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifests []Manifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(d.Root, "backups", entry.Name(), "manifest.json"))
		if err != nil {
			continue // incomplete backup
		}
		var manifest Manifest
		if json.Unmarshal(raw, &manifest) == nil {
			manifests = append(manifests, manifest)
		}
	}
	return manifests, nil
}

// Verify recomputes every checksum against the manifest.
func (d Deps) Verify(id string) (map[string]any, error) {
	if strings.ContainsAny(id, "/\\") {
		return nil, fmt.Errorf("invalid backup id")
	}
	dir := d.dir(id)
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("backup %q has no manifest", id)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	var problems []string
	for _, entry := range manifest.Files {
		sum, size, err := fileSHA256(filepath.Join(dir, entry.Name))
		switch {
		case err != nil:
			problems = append(problems, entry.Name+": missing")
		case size != entry.Bytes:
			problems = append(problems, fmt.Sprintf("%s: size %d != %d", entry.Name, size, entry.Bytes))
		case sum != entry.SHA256:
			problems = append(problems, entry.Name+": checksum mismatch")
		}
	}
	return map[string]any{"backup_id": id, "valid": len(problems) == 0, "problems": problems}, nil
}

type RestorePayload struct {
	BackupID string `json:"backup_id"`
}

// HandleRestore restores databases from a verified backup and unpacks the
// config archive over the live configuration. Databases are restored
// in-place (dumps apply onto the existing schema); a clean-slate restore is
// documented in docs/backup-restore.md.
func (d Deps) HandleRestore(ctx context.Context, payload json.RawMessage) (any, error) {
	var request RestorePayload
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, err
	}
	verification, err := d.Verify(request.BackupID)
	if err != nil {
		return nil, err
	}
	if verification["valid"] != true {
		return nil, fmt.Errorf("backup %q failed verification: %v", request.BackupID, verification["problems"])
	}

	if err := d.unpackConfig(request.BackupID); err != nil {
		return nil, fmt.Errorf("config restore: %w", err)
	}
	if _, err := d.Proxy.RunBackup(ctx, "restore", request.BackupID); err != nil {
		return nil, fmt.Errorf("database restore: %w", err)
	}
	return map[string]any{"backup_id": request.BackupID, "status": "restore started"}, nil
}

func (d Deps) unpackConfig(id string) error {
	source, err := os.Open(filepath.Join(d.dir(id), "config.tar.gz"))
	if err != nil {
		return err
	}
	defer source.Close()
	gz, err := gzip.NewReader(source)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		// path traversal guard
		clean := filepath.Clean(header.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("refusing archive entry %q", header.Name)
		}
		target := filepath.Join(d.Root, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode)&0o777)
		if err != nil {
			return err
		}
		if _, err := io.Copy(file, tr); err != nil {
			file.Close()
			return err
		}
		file.Close()
	}
}
