package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func newTestRoundRobinSelector() *RoundRobinSelector {
	return &RoundRobinSelector{
		cursors: make(map[string]int),
	}
}

func TestSessionAffinity_InitialPickBindsBeforeSuccess(t *testing.T) {
	authA := &Auth{ID: "auth-a"}
	authB := &Auth{ID: "auth-b"}
	auths := []*Auth{authA, authB}

	fallback := newTestRoundRobinSelector()
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{
		Headers: http.Header{"X-Session-Id": []string{"sess-12345678"}},
	}

	picked, err := selector.Pick(context.Background(), "provider", "model", opts, auths)
	if err != nil || picked == nil {
		t.Fatalf("Pick failed: err=%v, picked=%v", err, picked)
	}

	cacheKey := "provider::header:sess-12345678::model"
	bound, ok := selector.cache.Get(cacheKey)
	if !ok || bound != picked.ID {
		t.Fatalf("expected cacheKey %q to be pre-bound to %q, got bound=%q ok=%v", cacheKey, picked.ID, bound, ok)
	}
}

func TestSessionAffinity_SuccessfulResultBindsChosenAuth(t *testing.T) {
	authA := &Auth{ID: "auth-a"}
	authB := &Auth{ID: "auth-b"}
	auths := []*Auth{authA, authB}

	fallback := newTestRoundRobinSelector()
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{
		Headers: http.Header{"X-Session-Id": []string{"sess-12345678"}},
	}

	picked, err := selector.Pick(context.Background(), "provider", "model", opts, auths)
	if err != nil || picked == nil {
		t.Fatalf("Pick failed: err=%v, picked=%v", err, picked)
	}

	selector.OnResult(Result{
		AuthID:   picked.ID,
		Provider: "provider",
		Model:    "model",
		Success:  true,
		Options:  opts,
	})

	cacheKey := "provider::header:sess-12345678::model"
	bound, ok := selector.cache.Get(cacheKey)
	if !ok || bound != picked.ID {
		t.Fatalf("expected cacheKey %q to be bound to %q, got bound=%q ok=%v", cacheKey, picked.ID, bound, ok)
	}
}

func TestSessionAffinity_RetryableFailureInvalidatesMatchingBinding(t *testing.T) {
	authA := &Auth{ID: "auth-a"}

	fallback := newTestRoundRobinSelector()
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{
		Headers: http.Header{"X-Session-Id": []string{"sess-12345678"}},
	}

	// Bind authA first
	selector.OnResult(Result{
		AuthID:   authA.ID,
		Provider: "provider",
		Model:    "model",
		Success:  true,
		Options:  opts,
	})

	cacheKey := "provider::header:sess-12345678::model"
	if _, ok := selector.cache.Get(cacheKey); !ok {
		t.Fatalf("precondition failed: cache key should be bound")
	}

	// Retryable/transient failure (429 Rate Limit) must RETAIN the binding.
	selector.OnResult(Result{
		AuthID:   authA.ID,
		Provider: "provider",
		Model:    "model",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "rate limit"},
		Options:  opts,
	})

	bound, ok := selector.cache.Get(cacheKey)
	if !ok || bound != authA.ID {
		t.Fatalf("expected cacheKey %q to be retained on 429 failure; got bound=%q ok=%v", cacheKey, bound, ok)
	}
}

func TestSessionAffinity_StaleFailureCannotDeleteNewerSuccess(t *testing.T) {
	authA := &Auth{ID: "auth-a"}
	authB := &Auth{ID: "auth-b"}

	fallback := newTestRoundRobinSelector()
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{
		Headers: http.Header{"X-Session-Id": []string{"sess-shared-12345"}},
	}

	// Request 2 succeeds on Auth B and binds it
	selector.OnResult(Result{
		AuthID:   authB.ID,
		Provider: "provider",
		Model:    "model",
		Success:  true,
		Options:  opts,
	})

	// Request 1 (older execution) fails on Auth A with retryable error
	selector.OnResult(Result{
		AuthID:   authA.ID,
		Provider: "provider",
		Model:    "model",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests, Message: "rate limit"},
		Options:  opts,
	})

	// Cache MUST still point to Auth B
	cacheKey := "provider::header:sess-shared-12345::model"
	bound, ok := selector.cache.Get(cacheKey)
	if !ok || bound != authB.ID {
		t.Fatalf("stale failure for %q deleted newer binding %q; cache state bound=%q ok=%v", authA.ID, authB.ID, bound, ok)
	}
}

func TestSessionAffinity_ExhaustedRequestDoesNotLeaveLastFailedAuthBound(t *testing.T) {
	authA := &Auth{ID: "auth-a"}
	authB := &Auth{ID: "auth-b"}
	auths := []*Auth{authA, authB}

	fallback := newTestRoundRobinSelector()
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{
		Headers: http.Header{"X-Session-Id": []string{"sess-exhausted-123"}},
	}
	cacheKey := "provider::header:sess-exhausted-123::model"

	// Attempt 1: picks Auth A, fails 429 (transient - binding retained)
	picked1, _ := selector.Pick(context.Background(), "provider", "model", opts, auths)
	selector.OnResult(Result{
		AuthID:   picked1.ID,
		Provider: "provider",
		Model:    "model",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests},
		Options:  opts,
	})
	if bound, ok := selector.cache.Get(cacheKey); !ok || bound != picked1.ID {
		t.Fatalf("primary binding should be retained after transient 429; bound=%q ok=%v", bound, ok)
	}

	// Attempt 2 within request: excludes Auth A, picks Auth B as sticky fallback, fails 429
	opts2 := cliproxyexecutor.Options{
		Headers: http.Header{"X-Session-Id": []string{"sess-exhausted-123"}},
		Metadata: map[string]any{
			cliproxyexecutor.ExcludedAuthIDsMetadataKey: map[string]struct{}{picked1.ID: {}},
		},
	}
	picked2, _ := selector.Pick(context.Background(), "provider", "model", opts2, auths)
	if picked2.ID == picked1.ID {
		t.Fatalf("fallback pick returned excluded auth %q", picked1.ID)
	}
	selector.OnResult(Result{
		AuthID:   picked2.ID,
		Provider: "provider",
		Model:    "model",
		Success:  false,
		Error:    &Error{HTTPStatus: http.StatusTooManyRequests},
		Options:  opts2,
	})

	// Primary binding must still be the original auth; fallback is stored temporarily.
	if bound, ok := selector.cache.Get(cacheKey); !ok || bound != picked1.ID {
		t.Fatalf("primary binding should be retained after fallback transient 429; bound=%q ok=%v", bound, ok)
	}
	if fb, ok := selector.fallbackCache.Get(cacheKey); !ok || fb != picked2.ID {
		t.Fatalf("fallback cache should hold %q after fallback failure; got %q ok=%v", picked2.ID, fb, ok)
	}

	// Once the primary auth is available again, the session returns to it and clears the fallback.
	second, _ := selector.Pick(context.Background(), "provider", "model", opts, auths)
	if second.ID != picked1.ID {
		t.Fatalf("exhausted request should return to original binding %q, got %q", picked1.ID, second.ID)
	}
	if _, ok := selector.fallbackCache.Get(cacheKey); ok {
		t.Fatalf("temporary fallback should be cleared when primary recovers")
	}
}

func TestSessionAffinity_ExistingWithinRequestExclusionPreventsRepeat(t *testing.T) {
	authA := &Auth{ID: "auth-a"}
	authB := &Auth{ID: "auth-b"}
	auths := []*Auth{authA, authB}

	fallback := newTestRoundRobinSelector()
	selector := NewSessionAffinitySelector(fallback)

	opts1 := cliproxyexecutor.Options{
		Headers: http.Header{"X-Session-Id": []string{"sess-repeat-12345"}},
	}

	picked1, _ := selector.Pick(context.Background(), "provider", "model", opts1, auths)

	// Next attempt excludes Auth A
	opts2 := cliproxyexecutor.Options{
		Headers: http.Header{"X-Session-Id": []string{"sess-repeat-12345"}},
		Metadata: map[string]any{
			cliproxyexecutor.ExcludedAuthIDsMetadataKey: map[string]struct{}{picked1.ID: {}},
		},
	}

	picked2, _ := selector.Pick(context.Background(), "provider", "model", opts2, auths)
	if picked2.ID == picked1.ID {
		t.Fatalf("within-request exclusion failed: pick returned excluded auth %q", picked1.ID)
	}
}

func TestSessionAffinity_ClientValidationErrorDoesNotRotateAffinity(t *testing.T) {
	authA := &Auth{ID: "auth-a"}

	fallback := newTestRoundRobinSelector()
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{
		Headers: http.Header{"X-Session-Id": []string{"sess-client-err-1"}},
	}

	// Bind authA
	selector.OnResult(Result{
		AuthID:   authA.ID,
		Provider: "provider",
		Model:    "model",
		Success:  true,
		Options:  opts,
	})

	cacheKey := "provider::header:sess-client-err-1::model"

	// Client request validation error (400 Bad Request, request-scoped)
	selector.OnResult(Result{
		AuthID:   authA.ID,
		Provider: "provider",
		Model:    "model",
		Success:  false,
		Error:    &Error{Code: requestScopedErrorCode, HTTPStatus: http.StatusBadRequest, Message: "invalid param"},
		Options:  opts,
	})

	// Binding MUST be preserved for client request validation errors
	bound, ok := selector.cache.Get(cacheKey)
	if !ok || bound != authA.ID {
		t.Fatalf("client validation error removed session affinity; expected bound=%q, got bound=%q ok=%v", authA.ID, bound, ok)
	}
}

type pickFuncSelector func(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, available []*Auth) (*Auth, error)

func (f pickFuncSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
	return f(ctx, provider, model, opts, available)
}

func closedStreamChunks() <-chan cliproxyexecutor.StreamChunk {
	ch := make(chan cliproxyexecutor.StreamChunk)
	close(ch)
	return ch
}

func TestSessionAffinity_CachedAuthUnavailableRebindsFallback(t *testing.T) {
	authA := &Auth{ID: "auth-a"}
	authB := &Auth{ID: "auth-b"}

	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"sess-unavail-1234"}}}
	cacheKey := "provider::header:sess-unavail-1234::model"

	selector.OnResult(Result{AuthID: authA.ID, Provider: "provider", Model: "model", Success: true, Options: opts})
	picked, err := selector.Pick(context.Background(), "provider", "model", opts, []*Auth{authB})
	if err != nil || picked == nil || picked.ID != authB.ID {
		t.Fatalf("Pick = %v/%v, want B", picked, err)
	}

	// Primary binding stays with the original auth; the fallback is sticky but temporary.
	if bound, ok := selector.cache.Get(cacheKey); !ok || bound != authA.ID {
		t.Fatalf("expected primary binding to remain %q after fallback; got %q ok=%v", authA.ID, bound, ok)
	}
	if fb, ok := selector.fallbackCache.Get(cacheKey); !ok || fb != authB.ID {
		t.Fatalf("expected fallback cache to hold %q; got %q ok=%v", authB.ID, fb, ok)
	}
}

func TestSessionAffinity_FallbackBFailsLeavesCacheEmpty(t *testing.T) {
	authA := &Auth{ID: "auth-a"}
	authB := &Auth{ID: "auth-b"}

	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"sess-bfail-1234"}}}
	cacheKey := "provider::header:sess-bfail-1234::model"

	selector.OnResult(Result{AuthID: authA.ID, Provider: "provider", Model: "model", Success: true, Options: opts})

	// A unavailable -> fallback picks B.
	picked, _ := selector.Pick(context.Background(), "provider", "model", opts, []*Auth{authB})
	if picked.ID != authB.ID {
		t.Fatalf("Pick = %q, want B", picked.ID)
	}
	// B fails with a transient 429. The primary binding is retained and B is stored temporarily.
	selector.OnResult(Result{AuthID: picked.ID, Provider: "provider", Model: "model", Success: false, Error: &Error{HTTPStatus: http.StatusTooManyRequests}, Options: opts})

	if bound, ok := selector.cache.Get(cacheKey); !ok || bound != authA.ID {
		t.Fatalf("primary binding should be retained after fallback failure; got %q ok=%v", bound, ok)
	}
	if fb, ok := selector.fallbackCache.Get(cacheKey); !ok || fb != authB.ID {
		t.Fatalf("fallback cache should hold %q after transient failure; got %q ok=%v", authB.ID, fb, ok)
	}

	// Once A is available again, the session returns to it and clears the temporary fallback.
	second, _ := selector.Pick(context.Background(), "provider", "model", opts, []*Auth{authA, authB})
	if second.ID != authA.ID {
		t.Fatalf("second request should return to primary binding %q, got %q", authA.ID, second.ID)
	}
	if _, ok := selector.fallbackCache.Get(cacheKey); ok {
		t.Fatalf("temporary fallback should be cleared when primary recovers")
	}
}

func TestSessionAffinity_FallbackBSucceedsBindsTemporaryFallback(t *testing.T) {
	authA := &Auth{ID: "auth-a"}
	authB := &Auth{ID: "auth-b"}

	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"sess-bok-123456"}}}
	cacheKey := "provider::header:sess-bok-123456::model"

	selector.OnResult(Result{AuthID: authA.ID, Provider: "provider", Model: "model", Success: true, Options: opts})

	picked, _ := selector.Pick(context.Background(), "provider", "model", opts, []*Auth{authB})
	if picked.ID != authB.ID {
		t.Fatalf("Pick = %q, want B", picked.ID)
	}
	// Fallback success must not overwrite the primary binding; it is stored as a sticky temporary fallback.
	selector.OnResult(Result{AuthID: picked.ID, Provider: "provider", Model: "model", Success: true, Options: opts})

	if bound, ok := selector.cache.Get(cacheKey); !ok || bound != authA.ID {
		t.Fatalf("primary binding should remain %q after fallback success; got %q ok=%v", authA.ID, bound, ok)
	}
	if fb, ok := selector.fallbackCache.Get(cacheKey); !ok || fb != authB.ID {
		t.Fatalf("fallback cache should hold %q after success; got %q ok=%v", authB.ID, fb, ok)
	}

	// When the primary auth is available again, the session returns to it.
	second, _ := selector.Pick(context.Background(), "provider", "model", opts, []*Auth{authA, authB})
	if second.ID != authA.ID {
		t.Fatalf("second request should return to primary binding %q, got %q", authA.ID, second.ID)
	}
	if _, ok := selector.fallbackCache.Get(cacheKey); ok {
		t.Fatalf("temporary fallback should be cleared when primary recovers")
	}
}

func TestSessionAffinity_StreamSuccessThroughWrapperBinds(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, nil, nil)
	affinity := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: &RoundRobinSelector{}, TTL: time.Hour})
	defer affinity.Stop()
	manager.SetSelector(affinity)

	auth := &Auth{ID: "stream-auth", Provider: "stream-provider", Status: StatusActive}
	if _, err := manager.Register(WithSkipPersist(ctx), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"stream-sess-12345"}}}
	cacheKey := "stream-provider::header:stream-sess-12345::stream-model"

	chunk := cliproxyexecutor.StreamChunk{Payload: []byte("data: {\"id\":\"x\"}\n\n")}
	res := manager.wrapStreamResult(ctx, auth, "stream-provider", "stream-model", opts, nil, []cliproxyexecutor.StreamChunk{chunk}, closedStreamChunks(), OAuthModelAliasResult{}, false)
	for range res.Chunks {
	}

	bound, ok := affinity.cache.Get(cacheKey)
	if !ok || bound != auth.ID {
		t.Fatalf("stream success should bind auth; bound=%q ok=%v", bound, ok)
	}
}

func TestSessionAffinity_StreamFailureThroughWrapperInvalidates(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, nil, nil)
	affinity := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: &RoundRobinSelector{}, TTL: time.Hour})
	defer affinity.Stop()
	manager.SetSelector(affinity)

	auth := &Auth{ID: "stream-auth", Provider: "stream-provider", Status: StatusActive}
	if _, err := manager.Register(WithSkipPersist(ctx), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"stream-sess-12345"}}}
	cacheKey := "stream-provider::header:stream-sess-12345::stream-model"

	// Bind first.
	affinity.OnResult(Result{AuthID: auth.ID, Provider: "stream-provider", Model: "stream-model", Success: true, Options: opts})
	if _, ok := affinity.cache.Get(cacheKey); !ok {
		t.Fatalf("precondition: bound")
	}

	// Stream fails with a transient upstream error (503). The binding must be retained.
	errChunk := cliproxyexecutor.StreamChunk{Err: &Error{HTTPStatus: http.StatusServiceUnavailable}}
	res := manager.wrapStreamResult(ctx, auth, "stream-provider", "stream-model", opts, nil, []cliproxyexecutor.StreamChunk{errChunk}, closedStreamChunks(), OAuthModelAliasResult{}, false)
	for range res.Chunks {
	}

	if bound, ok := affinity.cache.Get(cacheKey); !ok || bound != auth.ID {
		t.Fatalf("stream failure should retain affinity; bound=%q ok=%v", bound, ok)
	}
}
func optsWithMixedNamespace(opts cliproxyexecutor.Options) cliproxyexecutor.Options {
	if opts.Metadata == nil {
		opts.Metadata = make(map[string]any)
	}
	opts.Metadata[cliproxyexecutor.SessionAffinityProviderMetadataKey] = "mixed"
	return opts
}

func TestSessionAffinity_MixedNamespace_PickRecordsAndOnResultBindsCanonicalKey(t *testing.T) {
	gemini := &Auth{ID: "gemini-auth", Provider: "gemini"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	// Mixed pool: selection provider is literally "mixed", actual auth provider is "gemini".
	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"mixed-sess-12345"}}, Metadata: map[string]any{}}
	picked, err := selector.Pick(context.Background(), "mixed", "model", opts, []*Auth{gemini})
	if err != nil || picked == nil || picked.ID != gemini.ID {
		t.Fatalf("Pick = %v/%v, want gemini", picked, err)
	}
	// Pick must have recorded the "mixed" namespace into the shared request-local metadata map.
	ns, _ := opts.Metadata[cliproxyexecutor.SessionAffinityProviderMetadataKey].(string)
	if ns != "mixed" {
		t.Fatalf("namespace metadata = %q, want %q", ns, "mixed")
	}

	// Successful OnResult binds under the canonical "mixed" key, not the actual provider.
	selector.OnResult(Result{AuthID: picked.ID, Provider: "gemini", Model: "model", Success: true, Options: opts})

	mixedKey := "mixed::header:mixed-sess-12345::model"
	geminiKey := "gemini::header:mixed-sess-12345::model"
	if _, ok := selector.cache.Get(mixedKey); !ok {
		t.Fatalf("expected binding under canonical mixed key")
	}
	if _, ok := selector.cache.Get(geminiKey); ok {
		t.Fatalf("must NOT bind under actual provider key")
	}

	// Next Pick under "mixed" hits the bound auth.
	next, _ := selector.Pick(context.Background(), "mixed", "model", opts, []*Auth{gemini})
	if next.ID != gemini.ID {
		t.Fatalf("second Pick = %q, want gemini", next.ID)
	}
}

func TestSessionAffinity_MixedNamespace_FailureRetainsBinding(t *testing.T) {
	gemini := &Auth{ID: "gemini-auth", Provider: "gemini"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := optsWithMixedNamespace(cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"mixed-fail-12345"}}})
	cacheKey := "mixed::header:mixed-fail-12345::model"

	picked, _ := selector.Pick(context.Background(), "mixed", "model", opts, []*Auth{gemini})
	// A 429 is transient: the canonical binding must be retained.
	selector.OnResult(Result{AuthID: picked.ID, Provider: "gemini", Model: "model", Success: false, Error: &Error{HTTPStatus: http.StatusTooManyRequests}, Options: opts})

	if bound, ok := selector.cache.Get(cacheKey); !ok || bound != picked.ID {
		t.Fatalf("mixed cache should retain binding after transient failure; bound=%q ok=%v", bound, ok)
	}
}

func TestSessionAffinity_MixedNamespace_StaleAuthRebindsFallback(t *testing.T) {
	authA := &Auth{ID: "auth-a", Provider: "gemini"}
	authB := &Auth{ID: "auth-b", Provider: "antigravity"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := optsWithMixedNamespace(cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"mixed-stale-12345"}}})
	cacheKey := "mixed::header:mixed-stale-12345::model"

	selector.OnResult(Result{AuthID: authA.ID, Provider: "gemini", Model: "model", Success: true, Options: opts})
	picked, _ := selector.Pick(context.Background(), "mixed", "model", opts, []*Auth{authB})
	if picked.ID != authB.ID {
		t.Fatalf("Pick = %q, want B", picked.ID)
	}

	// The primary binding stays with A; the fallback is sticky but temporary.
	if bound, ok := selector.cache.Get(cacheKey); !ok || bound != authA.ID {
		t.Fatalf("expected primary binding to remain %q; got %q ok=%v", authA.ID, bound, ok)
	}
	if fb, ok := selector.fallbackCache.Get(cacheKey); !ok || fb != authB.ID {
		t.Fatalf("expected fallback cache to hold %q; got %q ok=%v", authB.ID, fb, ok)
	}
}

func TestSessionAffinity_MixedNamespace_StaleFailureCannotDeleteNewerSuccess(t *testing.T) {
	authA := &Auth{ID: "auth-a", Provider: "gemini"}
	authB := &Auth{ID: "auth-b", Provider: "gemini"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := optsWithMixedNamespace(cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"mixed-stale-newer-1"}}})
	cacheKey := "mixed::header:mixed-stale-newer-1::model"

	// Newer request succeeds on B under the canonical mixed key.
	selector.OnResult(Result{AuthID: authB.ID, Provider: "gemini", Model: "model", Success: true, Options: opts})
	// Older request fails on A; must not delete B's newer binding.
	selector.OnResult(Result{AuthID: authA.ID, Provider: "gemini", Model: "model", Success: false, Error: &Error{HTTPStatus: http.StatusTooManyRequests}, Options: opts})

	bound, ok := selector.cache.Get(cacheKey)
	if !ok || bound != authB.ID {
		t.Fatalf("stale failure for %q deleted newer binding %q; bound=%q ok=%v", authA.ID, authB.ID, bound, ok)
	}
}

func TestSessionAffinity_MixedNamespace_StreamBindsAndRetainsCanonicalKey(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, nil, nil)
	affinity := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: &RoundRobinSelector{}, TTL: time.Hour})
	defer affinity.Stop()
	manager.SetSelector(affinity)

	auth := &Auth{ID: "stream-mixed-auth", Provider: "gemini", Status: StatusActive}
	if _, err := manager.Register(WithSkipPersist(ctx), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	opts := optsWithMixedNamespace(cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"stream-mixed-12345"}}})
	cacheKey := "mixed::header:stream-mixed-12345::stream-model"

	// Success binds under the canonical key.
	chunk := cliproxyexecutor.StreamChunk{Payload: []byte("data: {\"id\":\"x\"}\n\n")}
	res := manager.wrapStreamResult(ctx, auth, "gemini", "stream-model", opts, nil, []cliproxyexecutor.StreamChunk{chunk}, closedStreamChunks(), OAuthModelAliasResult{}, false)
	for range res.Chunks {
	}
	bound, ok := affinity.cache.Get(cacheKey)
	if !ok || bound != auth.ID {
		t.Fatalf("stream mixed success should bind canonical key; bound=%q ok=%v", bound, ok)
	}

	// A 503 stream failure is transient: the canonical binding must be retained.
	errChunk := cliproxyexecutor.StreamChunk{Err: &Error{HTTPStatus: http.StatusServiceUnavailable}}
	res2 := manager.wrapStreamResult(ctx, auth, "gemini", "stream-model", opts, nil, []cliproxyexecutor.StreamChunk{errChunk}, closedStreamChunks(), OAuthModelAliasResult{}, false)
	for range res2.Chunks {
	}
	if bound, ok := affinity.cache.Get(cacheKey); !ok || bound != auth.ID {
		t.Fatalf("stream mixed failure should retain canonical key; bound=%q ok=%v", bound, ok)
	}
}

func TestSessionAffinity_SingleProviderStillUsesActualProviderKey(t *testing.T) {
	auth := &Auth{ID: "single-auth", Provider: "claude"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"single-sess-12345"}}, Metadata: map[string]any{}}

	// Single provider: Pick records "claude" as namespace; OnResult binds under "claude".
	picked, _ := selector.Pick(context.Background(), "claude", "model", opts, []*Auth{auth})
	selector.OnResult(Result{AuthID: picked.ID, Provider: "claude", Model: "model", Success: true, Options: opts})

	cacheKey := "claude::header:single-sess-12345::model"
	bound, ok := selector.cache.Get(cacheKey)
	if !ok || bound != auth.ID {
		t.Fatalf("single-provider bind failed; bound=%q ok=%v", bound, ok)
	}
}

func TestSessionAffinity_MixedNamespace_SecondRequestReturnsToPrimary(t *testing.T) {
	authA := &Auth{ID: "auth-a", Provider: "gemini"}
	authB := &Auth{ID: "auth-b", Provider: "gemini"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := optsWithMixedNamespace(cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"mixed-second-12345"}}})
	cacheKey := "mixed::header:mixed-second-12345::model"

	// Bind A under the canonical key.
	selector.OnResult(Result{AuthID: authA.ID, Provider: "gemini", Model: "model", Success: true, Options: opts})

	// A is unavailable (only B in list) -> Pick selects B as sticky fallback.
	picked, _ := selector.Pick(context.Background(), "mixed", "model", opts, []*Auth{authB})
	if picked.ID != authB.ID {
		t.Fatalf("Pick = %q, want B", picked.ID)
	}
	// B fails with a transient 503. The primary binding to A is retained.
	selector.OnResult(Result{AuthID: picked.ID, Provider: "gemini", Model: "model", Success: false, Error: &Error{HTTPStatus: http.StatusServiceUnavailable}, Options: opts})
	if bound, ok := selector.cache.Get(cacheKey); !ok || bound != authA.ID {
		t.Fatalf("primary binding should be retained after fallback failure; got %q ok=%v", bound, ok)
	}
	if fb, ok := selector.fallbackCache.Get(cacheKey); !ok || fb != authB.ID {
		t.Fatalf("fallback cache should hold %q; got %q ok=%v", authB.ID, fb, ok)
	}

	// Once A is available again, the session returns to it and clears the temporary fallback.
	second, _ := selector.Pick(context.Background(), "mixed", "model", opts, []*Auth{authA, authB})
	if second.ID != authA.ID {
		t.Fatalf("second request should return to primary binding %q, got %q", authA.ID, second.ID)
	}
	if _, ok := selector.fallbackCache.Get(cacheKey); ok {
		t.Fatalf("temporary fallback should be cleared when primary recovers")
	}
}
func optsWithAffinityNamespaces(opts cliproxyexecutor.Options, provider, model string) cliproxyexecutor.Options {
	if opts.Metadata == nil {
		opts.Metadata = make(map[string]any)
	}
	opts.Metadata[cliproxyexecutor.SessionAffinityProviderMetadataKey] = provider
	opts.Metadata[cliproxyexecutor.SessionAffinityModelMetadataKey] = model
	return opts
}

func TestSessionAffinity_ModelNamespace_MixedRouteModelBindsCanonicalKey(t *testing.T) {
	gemini := &Auth{ID: "gemini-auth", Provider: "gemini"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"model-mixed-12345"}}, Metadata: map[string]any{}}

	// Pick under the route model records the model namespace before any upstream rewrite.
	picked, err := selector.Pick(context.Background(), "mixed", ".gemini-flash", opts, []*Auth{gemini})
	if err != nil || picked == nil || picked.ID != gemini.ID {
		t.Fatalf("Pick = %v/%v, want gemini", picked, err)
	}
	if ns, _ := opts.Metadata[cliproxyexecutor.SessionAffinityModelMetadataKey].(string); ns != ".gemini-flash" {
		t.Fatalf("model namespace = %q, want .gemini-flash", ns)
	}

	// Success result carries a rewritten upstream model; OnResult must key by route model.
	selector.OnResult(Result{AuthID: picked.ID, Provider: "gemini", Model: "gemini-3.5-flash-lite", Success: true, Options: opts})

	routeKey := "mixed::header:model-mixed-12345::.gemini-flash"
	rewrittenKey := "mixed::header:model-mixed-12345::gemini-3.5-flash-lite"
	if _, ok := selector.cache.Get(routeKey); !ok {
		t.Fatalf("expected binding under route-model canonical key")
	}
	if _, ok := selector.cache.Get(rewrittenKey); ok {
		t.Fatalf("must NOT bind under rewritten upstream model key")
	}

	// Next Pick for the same route model hits the bound auth.
	next, _ := selector.Pick(context.Background(), "mixed", ".gemini-flash", opts, []*Auth{gemini})
	if next.ID != gemini.ID {
		t.Fatalf("second Pick = %q, want gemini", next.ID)
	}
}

func TestSessionAffinity_ModelNamespace_SingleProviderAliasRewrite(t *testing.T) {
	auth := &Auth{ID: "single-auth", Provider: "claude"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"model-single-12345"}}, Metadata: map[string]any{}}

	picked, _ := selector.Pick(context.Background(), "claude", "op-4-mini", opts, []*Auth{auth})
	selector.OnResult(Result{AuthID: picked.ID, Provider: "claude", Model: "op-4-mini-rewritten", Success: true, Options: opts})

	routeKey := "claude::header:model-single-12345::op-4-mini"
	if _, ok := selector.cache.Get(routeKey); !ok {
		t.Fatalf("expected binding under route-model alias key")
	}
	next, _ := selector.Pick(context.Background(), "claude", "op-4-mini", opts, []*Auth{auth})
	if next.ID != auth.ID {
		t.Fatalf("second Pick = %q, want auth", next.ID)
	}
}

func TestSessionAffinity_ModelNamespace_FailureRetainsRouteBinding(t *testing.T) {
	auth := &Auth{ID: "auth-a", Provider: "gemini"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := optsWithAffinityNamespaces(cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"model-fail-12345"}}}, "mixed", ".gemini-flash")
	routeKey := "mixed::header:model-fail-12345::.gemini-flash"

	// Bind under the route-model canonical key.
	selector.OnResult(Result{AuthID: auth.ID, Provider: "gemini", Model: "gemini-3.5-flash-lite", Success: true, Options: opts})
	if _, ok := selector.cache.Get(routeKey); !ok {
		t.Fatalf("precondition: route binding should exist")
	}

	// 503 is transient: the canonical route binding must be retained.
	selector.OnResult(Result{AuthID: auth.ID, Provider: "gemini", Model: "gemini-3.5-flash-lite", Success: false, Error: &Error{HTTPStatus: http.StatusServiceUnavailable}, Options: opts})
	if bound, ok := selector.cache.Get(routeKey); !ok || bound != auth.ID {
		t.Fatalf("route binding should be retained after transient failure; bound=%q ok=%v", bound, ok)
	}
}

func TestSessionAffinity_ModelNamespace_StaleFailureCannotDeleteNewerSuccess(t *testing.T) {
	authA := &Auth{ID: "auth-a", Provider: "gemini"}
	authB := &Auth{ID: "auth-b", Provider: "gemini"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	opts := optsWithAffinityNamespaces(cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"model-stale-newer"}}}, "mixed", ".gemini-flash")
	routeKey := "mixed::header:model-stale-newer::.gemini-flash"

	// Newer request succeeds on B under the route-model key.
	selector.OnResult(Result{AuthID: authB.ID, Provider: "gemini", Model: "gemini-3.5-flash-lite", Success: true, Options: opts})
	// Older request fails on A; must not delete B's newer binding.
	selector.OnResult(Result{AuthID: authA.ID, Provider: "gemini", Model: "gemini-3.5-flash-lite", Success: false, Error: &Error{HTTPStatus: http.StatusTooManyRequests}, Options: opts})

	bound, ok := selector.cache.Get(routeKey)
	if !ok || bound != authB.ID {
		t.Fatalf("stale failure for %q deleted newer binding %q; bound=%q ok=%v", authA.ID, authB.ID, bound, ok)
	}
}

func TestSessionAffinity_ModelNamespace_StreamRewriteBindsAndRetainsRouteKey(t *testing.T) {
	ctx := context.Background()
	manager := NewManager(nil, nil, nil)
	affinity := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: &RoundRobinSelector{}, TTL: time.Hour})
	defer affinity.Stop()
	manager.SetSelector(affinity)

	auth := &Auth{ID: "stream-model-auth", Provider: "gemini", Status: StatusActive}
	if _, err := manager.Register(WithSkipPersist(ctx), auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	opts := optsWithAffinityNamespaces(cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"stream-model-12345"}}}, "mixed", ".gemini-flash")
	routeKey := "mixed::header:stream-model-12345::.gemini-flash"

	// Stream succeeds with a rewritten upstream model; must bind the route-model key.
	chunk := cliproxyexecutor.StreamChunk{Payload: []byte("data: {\"id\":\"x\"}\n\n")}
	res := manager.wrapStreamResult(ctx, auth, "gemini", "gemini-3.5-flash-lite", opts, nil, []cliproxyexecutor.StreamChunk{chunk}, closedStreamChunks(), OAuthModelAliasResult{}, false)
	for range res.Chunks {
	}
	bound, ok := affinity.cache.Get(routeKey)
	if !ok || bound != auth.ID {
		t.Fatalf("stream rewrite should bind route key; bound=%q ok=%v", bound, ok)
	}

	// A 503 stream failure is transient: the route key binding must be retained.
	errChunk := cliproxyexecutor.StreamChunk{Err: &Error{HTTPStatus: http.StatusServiceUnavailable}}
	res2 := manager.wrapStreamResult(ctx, auth, "gemini", "gemini-3.5-flash-lite", opts, nil, []cliproxyexecutor.StreamChunk{errChunk}, closedStreamChunks(), OAuthModelAliasResult{}, false)
	for range res2.Chunks {
	}
	if bound, ok := affinity.cache.Get(routeKey); !ok || bound != auth.ID {
		t.Fatalf("stream failure should retain route key; bound=%q ok=%v", bound, ok)
	}
}

func TestSessionAffinity_ModelNamespace_MetadataAbsentUsesResultModel(t *testing.T) {
	auth := &Auth{ID: "auth-a", Provider: "gemini"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)

	// No namespace metadata: OnResult must fall back to Result.Provider/Result.Model.
	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"model-compat-12345"}}}
	selector.OnResult(Result{AuthID: auth.ID, Provider: "gemini", Model: "gemini-model", Success: true, Options: opts})

	cacheKey := "gemini::header:model-compat-12345::gemini-model"
	bound, ok := selector.cache.Get(cacheKey)
	if !ok || bound != auth.ID {
		t.Fatalf("metadata-absent should key by Result.Provider/Model; bound=%q ok=%v", bound, ok)
	}
}

func TestSessionAffinity_RequestScoped400DoesNotQuarantine(t *testing.T) {
	authA := &Auth{ID: "auth-a", Provider: "gemini"}
	authB := &Auth{ID: "auth-b", Provider: "gemini"}
	fallback := pickFuncSelector(func(_ context.Context, _, _ string, _ cliproxyexecutor.Options, available []*Auth) (*Auth, error) {
		return available[0], nil
	})
	selector := NewSessionAffinitySelector(fallback)
	defer selector.Stop()

	opts := optsWithAffinityNamespaces(cliproxyexecutor.Options{Headers: http.Header{"X-Session-Id": []string{"cooldown-client-error"}}}, "mixed", ".gemini-flash")
	selector.OnResult(Result{
		AuthID:   authA.ID,
		Provider: "gemini",
		Model:    "gemini-3.6-flash",
		Success:  false,
		Error:    &Error{Code: requestScopedErrorCode, HTTPStatus: http.StatusBadRequest},
		Options:  opts,
	})

	got, err := selector.Pick(context.Background(), "mixed", ".gemini-flash", opts, []*Auth{authA, authB})
	if err != nil || got.ID != authA.ID {
		t.Fatalf("Pick after request-scoped 400 = %v/%v, want auth-a", got, err)
	}
}
