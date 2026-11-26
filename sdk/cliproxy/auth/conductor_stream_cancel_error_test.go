package auth

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type cancelStreamRequestScopedError struct {
	status  int
	message string
}

func (e cancelStreamRequestScopedError) Error() string         { return e.message }
func (e cancelStreamRequestScopedError) StatusCode() int       { return e.status }
func (e cancelStreamRequestScopedError) IsRequestScoped() bool { return true }

type cancelStreamRetryAfterError struct {
	status     int
	message    string
	retryAfter time.Duration
}

func (e cancelStreamRetryAfterError) Error() string   { return e.message }
func (e cancelStreamRetryAfterError) StatusCode() int { return e.status }
func (e cancelStreamRetryAfterError) RetryAfter() *time.Duration {
	d := e.retryAfter
	return &d
}

type cancelStreamTransientTransportError struct {
	status  int
	message string
}

func (e cancelStreamTransientTransportError) Error() string     { return e.message }
func (e cancelStreamTransientTransportError) StatusCode() int   { return e.status }
func (e cancelStreamTransientTransportError) ErrorCode() string { return ErrorCodeTransientTransport }

type cancelStreamResultHook struct {
	NoopHook
	authID  string
	mu      sync.Mutex
	results []Result
	notify  chan Result
}

func (h *cancelStreamResultHook) OnResult(_ context.Context, result Result) {
	if result.AuthID != h.authID {
		return
	}
	h.mu.Lock()
	h.results = append(h.results, result)
	h.mu.Unlock()
	if h.notify != nil {
		select {
		case h.notify <- result:
		default:
		}
	}
}

func (h *cancelStreamResultHook) snapshot() []Result {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Result, len(h.results))
	copy(out, h.results)
	return out
}

func setupConductorStreamCancelTest(t *testing.T, streamFn func(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error)) (*Manager, *Auth, string, *cancelStreamResultHook) {
	t.Helper()
	provider := "cancel-stream-" + uuid.NewString()
	model := "cancel-stream-model-" + uuid.NewString()
	auth := &Auth{
		ID:       "cancel-stream-auth-" + uuid.NewString(),
		Provider: provider,
		Status:   StatusActive,
	}
	hook := &cancelStreamResultHook{
		authID: auth.ID,
		notify: make(chan Result, 4),
	}
	manager := NewManager(nil, nil, hook)
	manager.SetRetryConfig(0, 0, 0)
	manager.RegisterExecutor(&customStreamMockExecutor{
		identifier: provider,
		streamFn:   streamFn,
	})
	registry.GetGlobalRegistry().RegisterClient(auth.ID, provider, []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})
	if _, errReg := manager.Register(context.Background(), auth); errReg != nil {
		t.Fatalf("Register() error = %v", errReg)
	}
	return manager, auth, model, hook
}

func assertChannelClosedPromptly(t *testing.T, ch <-chan cliproxyexecutor.StreamChunk, label string) {
	t.Helper()
	select {
	case _, open := <-ch:
		if open {
			t.Fatalf("%s unexpectedly delivered another chunk after cancel", label)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s to close after cancel", label)
	}
}

func assertSignalClosedPromptly(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestConductorStreamCancelRecordsPostCancelErrors(t *testing.T) {
	t.Run("request_scoped_error_after_cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		clientCanceled := make(chan struct{})
		producerDone := make(chan struct{})

		manager, auth, model, hook := setupConductorStreamCancelTest(t, func(execCtx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
			chunks := make(chan cliproxyexecutor.StreamChunk)
			go func() {
				defer close(producerDone)
				defer close(chunks)
				chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(`{"type":"content_block_delta","delta":{"text":"ok"}}`)}
				<-execCtx.Done()
				<-clientCanceled
				chunks <- cliproxyexecutor.StreamChunk{Err: cancelStreamRequestScopedError{
					status:  http.StatusBadGateway,
					message: "malformed function call",
				}}
				// A second trailing error chunk must be drained without recording a duplicate result.
				chunks <- cliproxyexecutor.StreamChunk{Err: cancelStreamRetryAfterError{
					status:     http.StatusTooManyRequests,
					message:    "duplicate trailing error",
					retryAfter: 5 * time.Minute,
				}}
			}()
			return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
		})

		streamRes, errStream := manager.ExecuteStream(ctx, []string{auth.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{Stream: true})
		if errStream != nil {
			t.Fatalf("ExecuteStream() error = %v", errStream)
		}
		firstChunk, ok := <-streamRes.Chunks
		if !ok || len(firstChunk.Payload) == 0 {
			t.Fatalf("expected initial payload chunk, got ok=%v chunk=%+v", ok, firstChunk)
		}

		cancel()
		assertChannelClosedPromptly(t, streamRes.Chunks, "streamRes.Chunks")

		close(clientCanceled)
		assertSignalClosedPromptly(t, producerDone, "producerDone")

		var recorded Result
		select {
		case recorded = <-hook.notify:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for OnResult failure recording")
		}
		if recorded.Success || recorded.Error == nil {
			t.Fatalf("OnResult = %+v, want failure with error", recorded)
		}
		if recorded.Error.HTTPStatus != http.StatusBadGateway {
			t.Fatalf("OnResult Error.HTTPStatus = %d, want 502", recorded.Error.HTTPStatus)
		}
		if recorded.Error.Code != ErrorCodeRequestScoped {
			t.Fatalf("OnResult Error.Code = %q, want %q", recorded.Error.Code, ErrorCodeRequestScoped)
		}
		if recorded.RetryAfter != nil {
			t.Fatalf("OnResult RetryAfter = %v, want nil", recorded.RetryAfter)
		}
		if got := hook.snapshot(); len(got) != 1 {
			t.Fatalf("OnResult call count = %d, want 1", len(got))
		}
		current, okAuth := manager.GetByID(auth.ID)
		if !okAuth || current == nil {
			t.Fatalf("GetByID(%q) missing auth", auth.ID)
		}
		if current.Failed != 1 || current.Success != 0 {
			t.Fatalf("auth counters = (Success=%d, Failed=%d), want (0, 1)", current.Success, current.Failed)
		}
		if current.Unavailable || !current.NextRetryAfter.IsZero() {
			t.Fatalf("request-scoped 502 unexpectedly cooled auth: unavailable=%v next=%v", current.Unavailable, current.NextRetryAfter)
		}
	})

	t.Run("retry_after_cooldown_after_cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		clientCanceled := make(chan struct{})
		producerDone := make(chan struct{})

		manager, auth, model, hook := setupConductorStreamCancelTest(t, func(execCtx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
			chunks := make(chan cliproxyexecutor.StreamChunk)
			go func() {
				defer close(producerDone)
				defer close(chunks)
				chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(`{"type":"response.output_text.delta","delta":"ok"}`)}
				<-execCtx.Done()
				<-clientCanceled
				chunks <- cliproxyexecutor.StreamChunk{Err: cancelStreamRetryAfterError{
					status:     http.StatusTooManyRequests,
					message:    "rate limit exceeded",
					retryAfter: 10 * time.Minute,
				}}
			}()
			return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
		})

		streamRes, errStream := manager.ExecuteStream(ctx, []string{auth.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{Stream: true})
		if errStream != nil {
			t.Fatalf("ExecuteStream() error = %v", errStream)
		}
		firstChunk, ok := <-streamRes.Chunks
		if !ok || len(firstChunk.Payload) == 0 {
			t.Fatalf("expected initial payload chunk, got ok=%v chunk=%+v", ok, firstChunk)
		}

		cancel()
		assertChannelClosedPromptly(t, streamRes.Chunks, "streamRes.Chunks")

		close(clientCanceled)
		assertSignalClosedPromptly(t, producerDone, "producerDone")

		var recorded Result
		select {
		case recorded = <-hook.notify:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for OnResult cooldown recording")
		}
		if recorded.Success || recorded.Error == nil {
			t.Fatalf("OnResult = %+v, want failure with error", recorded)
		}
		if recorded.Error.HTTPStatus != http.StatusTooManyRequests {
			t.Fatalf("OnResult Error.HTTPStatus = %d, want 429", recorded.Error.HTTPStatus)
		}
		if recorded.RetryAfter == nil || *recorded.RetryAfter != 10*time.Minute {
			t.Fatalf("OnResult RetryAfter = %v, want 10m", recorded.RetryAfter)
		}
		current, okAuth := manager.GetByID(auth.ID)
		if !okAuth || current == nil {
			t.Fatalf("GetByID(%q) missing auth", auth.ID)
		}
		if !current.Unavailable || !current.NextRetryAfter.After(time.Now().Add(5*time.Minute)) {
			t.Fatalf("auth was not cooled by RetryAfter error: unavailable=%v next=%v", current.Unavailable, current.NextRetryAfter)
		}
	})

	t.Run("transient_transport_error_after_cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		clientCanceled := make(chan struct{})
		producerDone := make(chan struct{})

		manager, auth, model, hook := setupConductorStreamCancelTest(t, func(execCtx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
			chunks := make(chan cliproxyexecutor.StreamChunk)
			go func() {
				defer close(producerDone)
				defer close(chunks)
				chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(`{"type":"content_block_delta","delta":{"text":"ok"}}`)}
				<-execCtx.Done()
				<-clientCanceled
				chunks <- cliproxyexecutor.StreamChunk{Err: cancelStreamTransientTransportError{
					status:  http.StatusBadGateway,
					message: "zero-output malformed function call",
				}}
			}()
			return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
		})

		streamRes, errStream := manager.ExecuteStream(ctx, []string{auth.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{Stream: true})
		if errStream != nil {
			t.Fatalf("ExecuteStream() error = %v", errStream)
		}
		firstChunk, ok := <-streamRes.Chunks
		if !ok || len(firstChunk.Payload) == 0 {
			t.Fatalf("expected initial payload chunk, got ok=%v chunk=%+v", ok, firstChunk)
		}

		cancel()
		assertChannelClosedPromptly(t, streamRes.Chunks, "streamRes.Chunks")

		close(clientCanceled)
		assertSignalClosedPromptly(t, producerDone, "producerDone")

		var recorded Result
		select {
		case recorded = <-hook.notify:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for OnResult failure recording")
		}
		if recorded.Success || recorded.Error == nil {
			t.Fatalf("OnResult = %+v, want failure with error", recorded)
		}
		if recorded.Error.HTTPStatus != http.StatusBadGateway {
			t.Fatalf("OnResult Error.HTTPStatus = %d, want 502", recorded.Error.HTTPStatus)
		}
		if recorded.Error.Code != ErrorCodeTransientTransport {
			t.Fatalf("OnResult Error.Code = %q, want %q", recorded.Error.Code, ErrorCodeTransientTransport)
		}
		current, okAuth := manager.GetByID(auth.ID)
		if !okAuth || current == nil {
			t.Fatalf("GetByID(%q) missing auth", auth.ID)
		}
		if current.Failed != 1 || current.Success != 0 {
			t.Fatalf("auth counters = (Success=%d, Failed=%d), want (0, 1)", current.Success, current.Failed)
		}
		if current.Unavailable || !current.NextRetryAfter.IsZero() {
			t.Fatalf("transient_transport 502 unexpectedly cooled auth: unavailable=%v next=%v", current.Unavailable, current.NextRetryAfter)
		}
	})

	t.Run("plain_error_after_cancel_does_not_record", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		clientCanceled := make(chan struct{})
		producerDone := make(chan struct{})

		manager, auth, model, hook := setupConductorStreamCancelTest(t, func(execCtx context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
			chunks := make(chan cliproxyexecutor.StreamChunk)
			go func() {
				defer close(producerDone)
				defer close(chunks)
				chunks <- cliproxyexecutor.StreamChunk{Payload: []byte(`{"type":"response.output_text.delta","delta":"ok"}`)}
				<-execCtx.Done()
				<-clientCanceled
				chunks <- cliproxyexecutor.StreamChunk{Err: errors.New("plain stream abort")}
				// Unbuffered sentinel ensures the drain goroutine finished inspecting the error chunk before producerDone closes.
				chunks <- cliproxyexecutor.StreamChunk{}
			}()
			return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
		})

		streamRes, errStream := manager.ExecuteStream(ctx, []string{auth.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{Stream: true})
		if errStream != nil {
			t.Fatalf("ExecuteStream() error = %v", errStream)
		}
		firstChunk, ok := <-streamRes.Chunks
		if !ok || len(firstChunk.Payload) == 0 {
			t.Fatalf("expected initial payload chunk, got ok=%v chunk=%+v", ok, firstChunk)
		}

		cancel()
		assertChannelClosedPromptly(t, streamRes.Chunks, "streamRes.Chunks")

		close(clientCanceled)
		assertSignalClosedPromptly(t, producerDone, "producerDone")

		if got := hook.snapshot(); len(got) != 0 {
			t.Fatalf("OnResult unexpectedly recorded %d result(s): %+v", len(got), got)
		}
		current, okAuth := manager.GetByID(auth.ID)
		if !okAuth || current == nil {
			t.Fatalf("GetByID(%q) missing auth", auth.ID)
		}
		if current.Failed != 0 || current.Success != 0 || current.Unavailable {
			t.Fatalf("auth state unexpectedly mutated on plain post-cancel error: Success=%d Failed=%d Unavailable=%v", current.Success, current.Failed, current.Unavailable)
		}
	})
}
