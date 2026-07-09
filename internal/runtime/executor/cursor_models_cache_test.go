package executor

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// withCursorModelsCache swaps the package-level cache for the duration of a test
// and restores the previous state on cleanup.
func withCursorModelsCache(t *testing.T, seed map[string]*modelsCacheEntry) {
	t.Helper()
	cursorModelsCacheMu.Lock()
	prev := cursorModelsCache
	cursorModelsCache = make(map[string]*modelsCacheEntry, len(seed))
	for k, v := range seed {
		cursorModelsCache[k] = v
	}
	cursorModelsCacheMu.Unlock()
	t.Cleanup(func() {
		cursorModelsCacheMu.Lock()
		cursorModelsCache = prev
		cursorModelsCacheMu.Unlock()
	})
}

// TestCursorModelsOrFallback_PrefersCacheOverHardcoded ensures that when a
// previous successful fetch cached models for an auth, a subsequent fetch
// failure returns the cached models instead of the hardcoded fallback.
// This prevents the live->fallback->live churn that removes working models
// (e.g. composer-2.5) from the registry after a transient network blip.
func TestCursorModelsOrFallback_PrefersCacheOverHardcoded(t *testing.T) {
	cached := []*registry.ModelInfo{
		{ID: "composer-2.5", Object: "model", OwnedBy: "cursor", Type: cursorAuthType, DisplayName: "Composer 2.5"},
	}
	withCursorModelsCache(t, map[string]*modelsCacheEntry{
		"auth-with-cache": {models: cloneModelsList(cached), createdAt: time.Now()},
	})

	got := cursorModelsOrFallback("auth-with-cache")
	if len(got) != 1 || got[0].ID != "composer-2.5" {
		t.Fatalf("expected cached [composer-2.5], got %+v", got)
	}

	// Unknown auth id with no cache entry must fall through to the hardcoded list.
	fb := cursorModelsOrFallback("never-seen-auth")
	if len(fb) == 0 {
		t.Fatal("expected non-empty hardcoded fallback for unknown auth")
	}
}

// TestFetchCursorModels_NilAuthReturnsFallback guards against the auth
// pointer being nil. cursorAccessToken already nil-guards, but
// authID := auth.ID is evaluated before that helper is called, so a nil
// auth would panic. The function should degrade to the hardcoded fallback
// instead of crashing the goroutine that reconciles the model registry.
func TestFetchCursorModels_NilAuthReturnsFallback(t *testing.T) {
	got := FetchCursorModels(context.Background(), nil, nil)
	if got == nil {
		t.Fatal("FetchCursorModels must return a non-nil slice for nil auth, got nil")
	}
	if len(got) == 0 {
		t.Fatal("FetchCursorModels must return the hardcoded fallback for nil auth, got empty slice")
	}
}

// TestCursorModelsOrFallback_ReturnIsDefensivelyCopied guards against
// the returned cache slice aliasing the stored one. If the caller
// replaces a slice element (e.g. `got[0] = newEntry`), the cache must
// remain intact; otherwise a later failure-path fetch could return the
// caller's mutated value instead of the real cached list.
func TestCursorModelsOrFallback_ReturnIsDefensivelyCopied(t *testing.T) {
	original := &registry.ModelInfo{ID: "cached-original", Object: "model", OwnedBy: "cursor", Type: cursorAuthType}
	withCursorModelsCache(t, map[string]*modelsCacheEntry{
		"auth-iso-get": {models: cloneModelsList([]*registry.ModelInfo{original}), createdAt: time.Now()},
	})

	got := cursorModelsOrFallback("auth-iso-get")
	if len(got) != 1 || got[0].ID != "cached-original" {
		t.Fatalf("setup: expected [cached-original], got %+v", got)
	}

	// Caller replaces slice element via the returned slice; the cache must
	// still hold the original element after this.
	got[0] = &registry.ModelInfo{ID: "caller-replaced"}

	again := cursorModelsOrFallback("auth-iso-get")
	if len(again) != 1 || again[0].ID != "cached-original" {
		t.Fatalf("cache was corrupted by caller element replacement: got %+v", again)
	}
}

// TestCacheCursorModels_StoresDefensiveCopy guards against the cache
// aliasing the caller's slice. If the caller replaces a slice element
// after caching, the cache must remain intact.
func TestCacheCursorModels_StoresDefensiveCopy(t *testing.T) {
	withCursorModelsCache(t, nil)

	models := []*registry.ModelInfo{
		{ID: "original", Object: "model", OwnedBy: "cursor", Type: cursorAuthType},
	}
	cacheCursorModels("auth-iso-set", models)

	// Mutate the caller's slice after caching; the cache must still hold
	// the original (uncorrupted) entry.
	models[0] = &registry.ModelInfo{ID: "caller-replaced"}

	got := cursorModelsOrFallback("auth-iso-set")
	if len(got) != 1 || got[0].ID != "original" {
		t.Fatalf("cache was corrupted by caller slice mutation: got %+v", got)
	}
}

// TestGetCursorFallbackModels_IsCurrent guards against the hardcoded list
// drifting to stale model ids and against it losing the models users
// actually call. Uses structural assertions rather than exact model IDs
// so it does not break when Cursor renames or retires individual models.
func TestGetCursorFallbackModels_IsCurrent(t *testing.T) {
	fb := GetCursorFallbackModels()
	if len(fb) == 0 {
		t.Fatal("hardcoded fallback must not be empty")
	}

	// All entries must be Cursor-owned (filtered by OwnedBy = "cursor"
	// in the fallback list builder).
	for _, m := range fb {
		if m.ID == "" {
			t.Errorf("fallback model has empty ID")
		}
		if m.OwnedBy != "cursor" {
			t.Errorf(
				"fallback model %q is owned by %q (expected %q); "+
					"if a non-Cursor model appears in the fallback, "+
					"the registry may collapse to an incomplete set",
				m.ID, m.OwnedBy, "cursor",
			)
		}
	}

	// No duplicate IDs
	ids := make(map[string]bool, len(fb))
	for _, m := range fb {
		if ids[m.ID] {
			t.Errorf("duplicate fallback model ID: %q", m.ID)
		}
		ids[m.ID] = true
	}

	// Must contain the model the user actually calls (composer-2.5):
	// this is the one model whose name is most stable and central to
	// the Cursor product.
	if !ids["composer-2.5"] {
		t.Errorf(
			"hardcoded fallback missing composer-2.5; " +
				"this is the model users actually call via cursor-cli",
		)
	}
}
