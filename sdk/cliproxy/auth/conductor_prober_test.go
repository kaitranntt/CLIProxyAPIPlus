package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type proberTestExecutor struct {
	provider      string
	statusCode    int
	body          string
	err           error
	calls         atomic.Int32
	respondStatus int
}

func (e *proberTestExecutor) Identifier() string { return e.provider }

func (e *proberTestExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e *proberTestExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (e *proberTestExecutor) Refresh(context.Context, *Auth) (*Auth, error) { return nil, nil }

func (e *proberTestExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e *proberTestExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	e.calls.Add(1)
	if e.err != nil {
		return nil, e.err
	}
	status := e.statusCode
	if status <= 0 {
		status = e.respondStatus
	}
	if status <= 0 {
		status = http.StatusOK
	}
	body := e.body
	if body == "" {
		body = "{}"
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestProberDisabledByDefault(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil, nil, nil)
	exec := &proberTestExecutor{provider: "test"}
	m.RegisterExecutor(exec)

	auth := &Auth{ID: "a1", Provider: "test", Status: StatusActive, Attributes: map[string]string{"base_url": "https://example.com"}}
	if _, err := m.Register(ctx, auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg := internalconfig.CredentialProberConfig{Enabled: false}
	m.SetConfig(&internalconfig.Config{CredentialProber: cfg})

	time.Sleep(100 * time.Millisecond)
	if exec.calls.Load() != 0 {
		t.Fatalf("prober called disabled executor %d times", exec.calls.Load())
	}
}

func TestProberSkipsDisabledAuth(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil, nil, nil)
	exec := &proberTestExecutor{provider: "test", err: fmt.Errorf("boom")}
	m.RegisterExecutor(exec)

	auth := &Auth{ID: "a1", Provider: "test", Status: StatusDisabled, Attributes: map[string]string{"base_url": "https://example.com"}}
	if _, err := m.Register(ctx, auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg := internalconfig.CredentialProberConfig{Enabled: true, Interval: time.Hour, Timeout: time.Second, MaxConcurrency: 1, RateLimitPerMinute: 1000}
	m.SetConfig(&internalconfig.Config{CredentialProber: cfg})

	time.Sleep(100 * time.Millisecond)
	if exec.calls.Load() != 0 {
		t.Fatalf("prober probed disabled auth %d times", exec.calls.Load())
	}
}

func TestProberMarksAuthUnavailableOnFailure(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil, nil, nil)
	exec := &proberTestExecutor{provider: "test", err: fmt.Errorf("upstream unreachable")}
	m.RegisterExecutor(exec)

	auth := &Auth{ID: "a1", Provider: "test", Status: StatusActive, Attributes: map[string]string{"base_url": "https://example.com"}}
	if _, err := m.Register(ctx, auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg := internalconfig.CredentialProberConfig{Enabled: true, Interval: time.Hour, Timeout: time.Second, MaxConcurrency: 1, RateLimitPerMinute: 1000}
	m.SetConfig(&internalconfig.Config{CredentialProber: cfg})

	// wait for the immediate first sweep
	time.Sleep(100 * time.Millisecond)

	m.mu.RLock()
	updated := m.auths["a1"]
	m.mu.RUnlock()

	if updated == nil {
		t.Fatal("auth disappeared")
	}
	if exec.calls.Load() != 1 {
		t.Fatalf("prober calls = %d, want 1", exec.calls.Load())
	}
	if !updated.Unavailable {
		t.Fatalf("auth.Unavailable = %v, want true", updated.Unavailable)
	}
	if updated.Status != StatusError {
		t.Fatalf("auth.Status = %v, want %v", updated.Status, StatusError)
	}
	if updated.NextRetryAfter.IsZero() {
		t.Fatalf("auth.NextRetryAfter not set after prober failure")
	}
}

func TestProberLeavesAuthActiveOnSuccess(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil, nil, nil)
	exec := &proberTestExecutor{provider: "test", statusCode: http.StatusOK}
	m.RegisterExecutor(exec)

	auth := &Auth{ID: "a1", Provider: "test", Status: StatusActive, Attributes: map[string]string{"base_url": "https://example.com"}}
	if _, err := m.Register(ctx, auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg := internalconfig.CredentialProberConfig{Enabled: true, Interval: time.Hour, Timeout: time.Second, MaxConcurrency: 1, RateLimitPerMinute: 1000}
	m.SetConfig(&internalconfig.Config{CredentialProber: cfg})

	time.Sleep(100 * time.Millisecond)

	m.mu.RLock()
	updated := m.auths["a1"]
	m.mu.RUnlock()

	if updated == nil {
		t.Fatal("auth disappeared")
	}
	if exec.calls.Load() != 1 {
		t.Fatalf("prober calls = %d, want 1", exec.calls.Load())
	}
	if updated.Unavailable {
		t.Fatalf("auth.Unavailable = %v, want false", updated.Unavailable)
	}
	if updated.Status != StatusActive {
		t.Fatalf("auth.Status = %v, want %v", updated.Status, StatusActive)
	}
}

func TestProberLeavesAuthActiveOnEmptyResponse(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil, nil, nil)
	exec := &proberTestExecutor{provider: "test", statusCode: http.StatusNoContent}
	m.RegisterExecutor(exec)

	auth := &Auth{ID: "a1", Provider: "test", Status: StatusActive, Attributes: map[string]string{"base_url": "https://example.com"}}
	if _, err := m.Register(ctx, auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg := internalconfig.CredentialProberConfig{Enabled: true, Interval: time.Hour, Timeout: time.Second, MaxConcurrency: 1, RateLimitPerMinute: 1000}
	m.SetConfig(&internalconfig.Config{CredentialProber: cfg})

	time.Sleep(100 * time.Millisecond)

	m.mu.RLock()
	updated := m.auths["a1"]
	m.mu.RUnlock()

	if updated == nil {
		t.Fatal("auth disappeared")
	}
	if updated.Unavailable {
		t.Fatalf("auth.Unavailable = %v, want false (204 is healthy for probe)", updated.Unavailable)
	}
	if updated.Status != StatusActive {
		t.Fatalf("auth.Status = %v, want %v", updated.Status, StatusActive)
	}
}

func TestProberLeavesAuthActiveOnEmpty200Response(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil, nil, nil)
	exec := &proberTestExecutor{provider: "test", statusCode: http.StatusOK, body: " "}
	m.RegisterExecutor(exec)

	auth := &Auth{ID: "a1", Provider: "test", Status: StatusActive, Attributes: map[string]string{"base_url": "https://example.com"}}
	if _, err := m.Register(ctx, auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cfg := internalconfig.CredentialProberConfig{Enabled: true, Interval: time.Hour, Timeout: time.Second, MaxConcurrency: 1, RateLimitPerMinute: 1000}
	m.SetConfig(&internalconfig.Config{CredentialProber: cfg})

	time.Sleep(100 * time.Millisecond)

	m.mu.RLock()
	updated := m.auths["a1"]
	m.mu.RUnlock()

	if updated == nil {
		t.Fatal("auth disappeared")
	}
	if updated.Unavailable {
		t.Fatalf("auth.Unavailable = %v, want false (empty 200 is healthy for probe)", updated.Unavailable)
	}
	if updated.Status != StatusActive {
		t.Fatalf("auth.Status = %v, want %v", updated.Status, StatusActive)
	}
}

type headerRecordingProberExecutor struct {
	proberTestExecutor
	lastReq *http.Request
}

func (e *headerRecordingProberExecutor) HttpRequest(ctx context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	if req != nil {
		e.lastReq = req.Clone(ctx)
	}
	return e.proberTestExecutor.HttpRequest(ctx, auth, req)
}

func TestProberSetsClaudeHeaders(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil, nil, nil)
	exec := &headerRecordingProberExecutor{proberTestExecutor: proberTestExecutor{provider: "claude"}}
	m.RegisterExecutor(exec)

	// OAuth credential without base_url
	authOAuth := &Auth{ID: "c-oauth", Provider: "claude", Status: StatusActive}
	if _, err := m.Register(ctx, authOAuth); err != nil {
		t.Fatalf("Register oauth: %v", err)
	}

	cfg := internalconfig.CredentialProberConfig{Enabled: true, Interval: time.Hour, Timeout: time.Second, MaxConcurrency: 1, RateLimitPerMinute: 1000}
	m.SetConfig(&internalconfig.Config{CredentialProber: cfg})

	time.Sleep(100 * time.Millisecond)

	if exec.lastReq == nil {
		t.Fatal("probe request was not executed")
	}
	if got := exec.lastReq.URL.String(); got != "https://api.anthropic.com/v1/models" {
		t.Fatalf("probe URL = %q, want https://api.anthropic.com/v1/models", got)
	}
	if got := exec.lastReq.Header.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Fatalf("Anthropic-Version = %q, want 2023-06-01", got)
	}
	if got := exec.lastReq.Header.Get("Anthropic-Beta"); got != "oauth-2025-04-20" {
		t.Fatalf("Anthropic-Beta = %q, want oauth-2025-04-20", got)
	}

	// API key credential should not have Anthropic-Beta set by default
	execAPIKey := &headerRecordingProberExecutor{proberTestExecutor: proberTestExecutor{provider: "claude"}}
	mAPIKey := NewManager(nil, nil, nil)
	mAPIKey.RegisterExecutor(execAPIKey)

	authAPIKey := &Auth{ID: "c-apikey", Provider: "claude", Status: StatusActive, Attributes: map[string]string{"api_key": "sk-ant-xxx"}}
	if _, err := mAPIKey.Register(ctx, authAPIKey); err != nil {
		t.Fatalf("Register api key: %v", err)
	}
	mAPIKey.SetConfig(&internalconfig.Config{CredentialProber: cfg})
	time.Sleep(100 * time.Millisecond)

	if execAPIKey.lastReq == nil {
		t.Fatal("probe request for api key was not executed")
	}
	if got := execAPIKey.lastReq.Header.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Fatalf("Anthropic-Version = %q, want 2023-06-01", got)
	}
	if got := execAPIKey.lastReq.Header.Get("Anthropic-Beta"); got != "" {
		t.Fatalf("Anthropic-Beta = %q, want empty for API key", got)
	}
}

func TestProberNoDeadlockOnConfigChange(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil, nil, nil)
	exec := &proberTestExecutor{provider: "test"}
	m.RegisterExecutor(exec)

	auth := &Auth{ID: "a1", Provider: "test", Status: StatusActive, Attributes: map[string]string{"base_url": "https://example.com"}}
	if _, err := m.Register(ctx, auth); err != nil {
		t.Fatalf("Register: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			enabled := (i % 2) == 0
			cfg := internalconfig.CredentialProberConfig{
				Enabled:            enabled,
				Interval:           10 * time.Millisecond,
				Timeout:            time.Second,
				MaxConcurrency:     2,
				RateLimitPerMinute: 600,
			}
			m.SetConfig(&internalconfig.Config{CredentialProber: cfg})
			m.SetConfigSnapshot(&internalconfig.Config{CredentialProber: cfg})
		}
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock detected during SetConfig / SetConfigSnapshot with prober enabled/disabled")
	}
}
