package support

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentRedaction(t *testing.T) {
	result := string(redactedEnvironment([]byte("SOVEREIGN_VERSION=1.2.3\nLITELLM_MASTER_KEY=secret\nDATABASE_URL=postgres://secret\n")))
	if !strings.Contains(result, "SOVEREIGN_VERSION=1.2.3") || strings.Contains(result, "secret") {
		t.Fatalf("redaction failed: %s", result)
	}
}

func TestBundleExcludesSecretsAndIncludesSafeOperationTimeline(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "logs", "hostd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SOVEREIGN_VERSION=1.2.3\nLITELLM_MASTER_KEY=super-secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "runtime.yaml"), []byte("schema_version: '1.1'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "logs", "hostd", "operations.jsonl"), []byte("{\"operation\":\"repair\",\"outcome\":\"succeeded\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	deps := &Deps{Root: root, Version: "1.2.3", Profile: "test"}
	result, err := deps.Handle(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	bundle := result.(Bundle)
	file, err := os.Open(filepath.Join(root, "support", "sovereign-support-"+bundle.ID+".tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewReader(gzipReader)
	contents := strings.Builder{}
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		contents.WriteString(header.Name)
		_, _ = io.Copy(&contents, archive)
	}
	text := contents.String()
	if strings.Contains(text, "super-secret-value") || !strings.Contains(text, "LITELLM_MASTER_KEY=<redacted>") {
		t.Fatalf("support bundle environment was not redacted: %s", text)
	}
	if !strings.Contains(text, "host-operations.jsonl") || !strings.Contains(text, `"operation":"repair"`) {
		t.Fatalf("support bundle omitted safe host operation timeline: %s", text)
	}
}
