package backups

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyPersistsTheLatestIntegrityState(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "backups", "backup-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(dir, "control.sql.gz")
	if err := os.WriteFile(payload, []byte("verified payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	sha, size, err := fileSHA256(payload)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &Manifest{
		ID: "backup-1", CreatedAt: "2026-08-17T00:00:00Z",
		Files: []ManifestFile{{Name: filepath.Base(payload), Bytes: size, SHA256: sha}},
	}
	if err := writeManifestFile(filepath.Join(dir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Root: root}
	result, err := deps.Verify("backup-1")
	if err != nil || result["valid"] != true {
		t.Fatalf("verify result=%v err=%v", result, err)
	}
	listed, err := deps.List()
	if err != nil || len(listed) != 1 || listed[0].VerificationState != "valid" || listed[0].VerifiedAt == "" {
		t.Fatalf("listed verified backup=%+v err=%v", listed, err)
	}

	if err := os.WriteFile(payload, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = deps.Verify("backup-1")
	if err != nil || result["valid"] != false {
		t.Fatalf("invalid result=%v err=%v", result, err)
	}
	listed, err = deps.List()
	if err != nil || listed[0].VerificationState != "invalid" {
		t.Fatalf("listed invalid backup=%+v err=%v", listed, err)
	}
}
