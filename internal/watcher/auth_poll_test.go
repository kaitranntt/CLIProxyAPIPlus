package watcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPollAuthDirOnceDetectsAddModifyDelete(t *testing.T) {
	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8317\n"), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	w, err := NewWatcher(configPath, authDir, nil)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer func() { _ = w.Stop() }()
	w.SetConfig(&config.Config{AuthDir: authDir})

	authPath := filepath.Join(authDir, "kiro-1.json")
	normalized := w.normalizeAuthPath(authPath)

	// Added file is picked up by polling.
	if err := os.WriteFile(authPath, []byte(`{"type":"kiro","access_token":"at-1"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	w.pollAuthDirOnce()
	w.clientsMutex.RLock()
	hashV1, ok := w.lastAuthHashes[normalized]
	w.clientsMutex.RUnlock()
	if !ok {
		t.Fatal("expected added auth file to be tracked after poll")
	}

	// Modified content updates the tracked hash.
	if err := os.WriteFile(authPath, []byte(`{"type":"kiro","access_token":"at-2"}`), 0o600); err != nil {
		t.Fatalf("modify auth file: %v", err)
	}
	w.pollAuthDirOnce()
	w.clientsMutex.RLock()
	hashV2 := w.lastAuthHashes[normalized]
	w.clientsMutex.RUnlock()
	if hashV2 == hashV1 {
		t.Fatal("expected hash to change after content modification")
	}

	// Deleted file is removed from tracking.
	if err := os.Remove(authPath); err != nil {
		t.Fatalf("remove auth file: %v", err)
	}
	w.pollAuthDirOnce()
	w.clientsMutex.RLock()
	_, stillTracked := w.lastAuthHashes[normalized]
	w.clientsMutex.RUnlock()
	if stillTracked {
		t.Fatal("expected deleted auth file to be untracked after poll")
	}
}
