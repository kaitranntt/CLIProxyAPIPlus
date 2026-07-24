package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJSONFileVerified_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	payload := []byte(`{"type":"kiro","access_token":"at"}`)

	if err := WriteJSONFileVerified(path, payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	persisted, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read back: %v", errRead)
	}
	if string(persisted) != string(payload) {
		t.Fatalf("expected persisted content to match, got %s", persisted)
	}
}

func TestWriteJSONFileVerified_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"v":1}`), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	payload := []byte(`{"v":2}`)
	if err := WriteJSONFileVerified(path, payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	persisted, errRead := os.ReadFile(path)
	if errRead != nil {
		t.Fatalf("read back: %v", errRead)
	}
	if string(persisted) != string(payload) {
		t.Fatalf("expected overwritten content, got %s", persisted)
	}
}

func TestWriteJSONFileVerified_RejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := WriteJSONFileVerified(path, []byte(`{"unclosed":`), 0o600); err == nil {
		t.Fatal("expected error when persisted content cannot be valid JSON")
	}
}
