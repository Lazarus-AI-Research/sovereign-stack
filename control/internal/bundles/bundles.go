// Package bundles creates and inventories same-platform offline distribution
// bundles. It archives only reviewed release assets, fixed image references,
// optional model caches, and the Metal agent; live config and secrets are never
// included.
package bundles

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Lazarus-AI-Research/sovereign-stack/control/internal/dockerproxy"
)

type Artifact struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Manifest struct {
	SchemaVersion   string     `json:"schema_version"`
	BundleID        string     `json:"bundle_id"`
	Version         string     `json:"version"`
	Profile         string     `json:"profile"`
	Architecture    string     `json:"architecture"`
	CreatedAt       string     `json:"created_at"`
	IncludesWeights bool       `json:"includes_weights"`
	Images          []Artifact `json:"images"`
	Models          []Artifact `json:"models"`
	Files           []Artifact `json:"files"`
}

type CreatePayload struct {
	Profile       string   `json:"profile,omitempty"`
	IncludeModels []string `json:"include_models,omitempty"`
}

type Deps struct {
	Root        string
	ReleaseRoot string
	Version     string
	Profile     string
	Images      []string
	Proxy       *dockerproxy.Client
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func randomSuffix() (string, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func shaFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), size, err
}

func addPath(tw *tar.Writer, root, path string, seen map[string]bool) error {
	return filepath.Walk(path, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name, err := filepath.Rel(root, current)
		if err != nil || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("path escapes archive root: %s", current)
		}
		if name == "." {
			return nil
		}
		name = filepath.ToSlash(name)
		if seen[name] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		seen[name] = true
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(current)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = name
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(current)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func archivePaths(destination, root string, relatives []string) (err error) {
	temporary := destination + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			file.Close()
			os.Remove(temporary)
		}
	}()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	seen := map[string]bool{}
	for _, relative := range relatives {
		if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe archive path %q", relative)
		}
		path := filepath.Join(root, relative)
		if _, statErr := os.Lstat(path); statErr != nil {
			return statErr
		}
		if err = addPath(tw, root, path, seen); err != nil {
			return err
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, destination)
}

func modelPaths(profile string, requested []string) ([]string, error) {
	var paths []string
	for _, id := range requested {
		switch id {
		case "all":
			return []string{"."}, nil
		case "assistant-large":
			if profile == "metal-arm64" {
				paths = append(paths, "metal/gemma-4-E2B_q4_0-it.gguf", "metal/gemma-4-E2B-it-mmproj.gguf")
			} else {
				paths = append(paths, "hf/hub/models--google--gemma-4-E2B-it")
			}
		case "embedding-omni-default":
			if profile != "cuda-x86_64" {
				return nil, fmt.Errorf("%s is not supported by %s", id, profile)
			}
			paths = append(paths, "hf/hub/models--LCO-Embedding--LCO-Embedding-Omni-3B-2605")
		case "embedding-text-compact":
			if profile == "metal-arm64" {
				paths = append(paths, "metal/nomic-embed-text-v1.5.Q8_0.gguf")
			} else {
				paths = append(paths, "hf/hub/models--nomic-ai--nomic-embed-text-v1.5")
			}
		default:
			return nil, fmt.Errorf("unknown model id %q", id)
		}
	}
	return paths, nil
}

func fileArtifact(path string) (Artifact, error) {
	hash, size, err := shaFile(path)
	return Artifact{Name: filepath.Base(path), Source: filepath.Base(path), SHA256: hash, Bytes: size}, err
}

func (d *Deps) HandleCreate(ctx context.Context, raw json.RawMessage) (any, error) {
	var request CreatePayload
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
	}
	if request.Profile == "" {
		request.Profile = d.Profile
	}
	if request.Profile != d.Profile || (d.Profile != "cuda-x86_64" && d.Profile != "metal-arm64") {
		return nil, fmt.Errorf("same-platform bundles only: installed profile is %s", d.Profile)
	}
	if d.Proxy == nil || len(d.Images) == 0 {
		return nil, fmt.Errorf("image export is unavailable")
	}
	suffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%s-%s-%s", d.Version, d.Profile, suffix)
	bundleRoot := filepath.Join(d.Root, "bundles")
	stage := filepath.Join(bundleRoot, "."+id)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	if err := os.WriteFile(filepath.Join(stage, "profile"), []byte(d.Profile+"\n"), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(stage, "version"), []byte(d.Version+"\n"), 0o644); err != nil {
		return nil, err
	}
	if err := archivePaths(filepath.Join(stage, "release.tar.gz"), d.ReleaseRoot, []string{"."}); err != nil {
		return nil, fmt.Errorf("release archive: %w", err)
	}
	imagesPath := filepath.Join(stage, "images.tar")
	if err := d.Proxy.ExportImages(ctx, d.Images, imagesPath); err != nil {
		return nil, err
	}

	manifest := Manifest{
		SchemaVersion: "1.0", BundleID: id, Version: d.Version, Profile: d.Profile,
		Architecture: "amd64", CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Images: []Artifact{}, Models: []Artifact{}, Files: []Artifact{},
	}
	if d.Profile == "metal-arm64" {
		manifest.Architecture = "arm64"
		agentRoot := filepath.Join(d.Root, "runtime-dist", d.Version)
		if err := archivePaths(filepath.Join(stage, "metal-agent.tar.gz"), agentRoot, []string{"."}); err != nil {
			return nil, fmt.Errorf("Metal agent archive: %w", err)
		}
	}
	if len(request.IncludeModels) > 0 {
		paths, err := modelPaths(d.Profile, request.IncludeModels)
		if err != nil {
			return nil, err
		}
		weightsPath := filepath.Join(stage, "weights.tar.gz")
		if err := archivePaths(weightsPath, filepath.Join(d.Root, "models"), paths); err != nil {
			return nil, fmt.Errorf("model archive: %w", err)
		}
		manifest.IncludesWeights = true
		artifact, err := fileArtifact(weightsPath)
		if err != nil {
			return nil, err
		}
		for _, id := range request.IncludeModels {
			modelArtifact := artifact
			modelArtifact.Name = id
			manifest.Models = append(manifest.Models, modelArtifact)
		}
	}

	imagesArtifact, err := fileArtifact(imagesPath)
	if err != nil {
		return nil, err
	}
	for index, ref := range d.Images {
		artifact := imagesArtifact
		artifact.Name = fmt.Sprintf("image-%02d", index+1)
		artifact.Source = ref
		manifest.Images = append(manifest.Images, artifact)
	}
	for _, name := range []string{"release.tar.gz", "images.tar", "weights.tar.gz", "metal-agent.tar.gz"} {
		path := filepath.Join(stage, name)
		if _, err := os.Stat(path); err == nil {
			artifact, err := fileArtifact(path)
			if err != nil {
				return nil, err
			}
			manifest.Files = append(manifest.Files, artifact)
		}
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	manifestRaw = append(manifestRaw, '\n')
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), manifestRaw, 0o644); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(stage)
	if err != nil {
		return nil, err
	}
	var checksumLines []string
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "checksums.sha256" {
			continue
		}
		hash, _, err := shaFile(filepath.Join(stage, entry.Name()))
		if err != nil {
			return nil, err
		}
		checksumLines = append(checksumLines, fmt.Sprintf("%s  %s", hash, entry.Name()))
	}
	sort.Strings(checksumLines)
	if err := os.WriteFile(filepath.Join(stage, "checksums.sha256"), []byte(strings.Join(checksumLines, "\n")+"\n"), 0o644); err != nil {
		return nil, err
	}

	archive := filepath.Join(bundleRoot, id+".tar.gz")
	if err := archivePaths(archive, stage, []string{"."}); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, id+".json"), manifestRaw, 0o644); err != nil {
		return nil, err
	}
	_, bytes, err := shaFile(archive)
	if err != nil {
		return nil, err
	}
	return map[string]any{"bundle_id": id, "archive": filepath.Base(archive), "bytes": bytes}, nil
}

func (d *Deps) List() ([]Manifest, error) {
	entries, err := os.ReadDir(filepath.Join(d.Root, "bundles"))
	if os.IsNotExist(err) {
		return []Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	var manifests []Manifest
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(d.Root, "bundles", entry.Name()))
		if err != nil {
			continue
		}
		var manifest Manifest
		if json.Unmarshal(raw, &manifest) == nil {
			manifests = append(manifests, manifest)
		}
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].CreatedAt > manifests[j].CreatedAt })
	return manifests, nil
}

func (d *Deps) Get(id string) (*Manifest, error) {
	if !safeID.MatchString(id) {
		return nil, fmt.Errorf("invalid bundle id")
	}
	raw, err := os.ReadFile(filepath.Join(d.Root, "bundles", id+".json"))
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (d *Deps) ArchivePath(id string) (string, error) {
	if _, err := d.Get(id); err != nil {
		return "", err
	}
	path := filepath.Join(d.Root, "bundles", id+".tar.gz")
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}
