package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// ttftScope owns exactly one TTFT attempt with a single-winner decision
// between stream establishment and the timeout fire. A fresh scope is
// created for every attempt, including the in-function retry after a
// credential refresh, so each retry gets a full TTFT budget. Once the stream
// is established, the timer is stopped and can never cancel the connected
// stream afterward; if the timer fired first, callers observe a typed TTFT
// timeout error and failover as before.
type ttftScope struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	fired     bool
	committed bool
	timer     *time.Timer
	timeout   time.Duration
}

func newTTFTScope(parent context.Context, timeout time.Duration) *ttftScope {
	var attemptCtx context.Context
	cancel := func() {}
	if parent != nil {
		attemptCtx, cancel = context.WithCancel(parent)
	}
	s := &ttftScope{
		ctx:     attemptCtx,
		cancel:  cancel,
		timeout: timeout,
	}
	if timeout > 0 {
		s.timer = time.AfterFunc(timeout, s.fire)
	}
	return s
}

// ctxOr returns the scope's fresh child context, falling back to ctx when the
// scope has none (nil parent).
func (s *ttftScope) ctxOr(ctx context.Context) context.Context {
	if s != nil && s.ctx != nil {
		return s.ctx
	}
	return ctx
}

// fire is the timer callback. It is a single winner: only the timer can set
// fired, and only while the scope has not already been committed by a first
// chunk.
func (s *ttftScope) fire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.committed || s.fired {
		return
	}
	s.fired = true
	if s.cancel != nil {
		s.cancel()
	}
}

// stop halts the TTFT timer once the upstream executor stream is established,
// ensuring the deadline only guards time-to-first-connect and never cancels a
// connected stream during subsequent chunk reads.
func (s *ttftScope) stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.committed = true
	if s.timer != nil {
		s.timer.Stop()
	}
}

// commit marks the first chunk as the winner and stops the timer so a later
// callback can never cancel a stream that already produced its first chunk.
// It returns a release func that the stream producer invokes after handoff to
// free the child context. commit is idempotent: the release func of a repeated
// call is a no-op.
func (s *ttftScope) commit() func() {
	if s == nil {
		return func() {}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.committed {
		return func() {}
	}
	s.committed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	cancel := s.cancel
	return func() {
		if cancel != nil {
			cancel()
		}
	}
}

// timedOut reports whether the timeout fired before the first chunk.
func (s *ttftScope) timedOut() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fired
}

// timeoutError returns a typed TTFT timeout error if the timeout won.
func (s *ttftScope) timeoutError() error {
	if s == nil || !s.timedOut() {
		return nil
	}
	return newTTFTTimeoutError(s.timeout)
}

// release cancels the child context and stops the timer. It races safely with
// a timer callback still in flight and is idempotent.
func (s *ttftScope) release() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.committed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func newTTFTTimeoutError(timeout time.Duration) error {
	return &Error{
		Code:       "stream_first_chunk_timeout",
		Message:    fmt.Sprintf("time to first chunk timeout after %v", timeout),
		HTTPStatus: 504,
		Retryable:  true,
	}
}

func (m *Manager) streamFirstChunkTimeout(opts cliproxyexecutor.Options) time.Duration {
	if opts.Metadata != nil {
		if ms, ok := opts.Metadata["stream_connect_timeout_ms"].(int); ok {
			if ms <= 0 {
				return 0
			}
			return time.Duration(ms) * time.Millisecond
		}
		if ms, ok := opts.Metadata["stream_first_chunk_timeout_ms"].(int); ok {
			if ms <= 0 {
				return 0
			}
			return time.Duration(ms) * time.Millisecond
		}
	}
	if m == nil {
		return 0
	}
	cfg, _ := m.runtimeConfig.Load().(*internalconfig.Config)
	if cfg == nil {
		return 0
	}
	if cfg.Streaming.StreamConnectTimeoutSeconds > 0 {
		return time.Duration(cfg.Streaming.StreamConnectTimeoutSeconds) * time.Second
	}
	if cfg.Streaming.StreamFirstChunkTimeoutSeconds > 0 {
		return time.Duration(cfg.Streaming.StreamFirstChunkTimeoutSeconds) * time.Second
	}
	return 0
}

func discardStreamChunks(ch <-chan cliproxyexecutor.StreamChunk) {
	if ch == nil {
		return
	}
	go func() {
		for range ch {
		}
	}()
}

type streamBootstrapError struct {
	cause   error
	headers http.Header
}

func cloneHTTPHeader(headers http.Header) http.Header {
	if headers == nil {
		return nil
	}
	return headers.Clone()
}

func newStreamBootstrapError(err error, headers http.Header) error {
	if err == nil {
		return nil
	}
	upstreamAttempt := hasUpstreamExecutionAttempt(err)
	err = unwrapUpstreamExecutionAttempt(err)
	bootstrapErr := &streamBootstrapError{
		cause:   err,
		headers: cloneHTTPHeader(headers),
	}
	if upstreamAttempt {
		return markUpstreamExecutionAttempt(bootstrapErr)
	}
	return bootstrapErr
}

func (e *streamBootstrapError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *streamBootstrapError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *streamBootstrapError) Headers() http.Header {
	if e == nil {
		return nil
	}
	return cloneHTTPHeader(e.headers)
}

func streamErrorResult(headers http.Header, err error) *cliproxyexecutor.StreamResult {
	ch := make(chan cliproxyexecutor.StreamChunk, 1)
	ch <- cliproxyexecutor.StreamChunk{Err: err}
	close(ch)
	return &cliproxyexecutor.StreamResult{
		Headers: cloneHTTPHeader(headers),
		Chunks:  ch,
	}
}

func validateStreamResult(result *cliproxyexecutor.StreamResult, err error) (*cliproxyexecutor.StreamResult, error) {
	if err != nil {
		return result, err
	}
	if result == nil || result.Chunks == nil {
		return result, &Error{Code: "empty_stream", Message: "upstream stream has no source", Retryable: true}
	}
	return result, nil
}

func readStreamBootstrap(ctx context.Context, ch <-chan cliproxyexecutor.StreamChunk, onFirstChunk ...func()) ([]cliproxyexecutor.StreamChunk, bool, error) {
	if ch == nil {
		return nil, true, nil
	}
	buffered := make([]cliproxyexecutor.StreamChunk, 0, 1)
	var bootstrap streamBootstrapState
	for {
		var (
			chunk cliproxyexecutor.StreamChunk
			ok    bool
		)
		if ctx != nil {
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case chunk, ok = <-ch:
			}
		} else {
			chunk, ok = <-ch
		}
		if !ok {
			return buffered, true, nil
		}
		if chunk.Err != nil {
			if bootstrap.hasMeaningfulOutput() {
				buffered = append(buffered, chunk)
				return buffered, false, nil
			}
			return nil, false, chunk.Err
		}
		for _, cb := range onFirstChunk {
			if cb != nil {
				cb()
			}
		}
		buffered = append(buffered, chunk)
		if cliproxyexecutor.DownstreamWebsocket(ctx) || bootstrap.observe(chunk.Payload) {
			return buffered, false, nil
		}
		if bootstrap.isTerminalEmpty() {
			return buffered, true, nil
		}
	}
}

func (m *Manager) wrapStreamResult(ctx context.Context, auth *Auth, provider, resultModel, routeModel string, headers http.Header, buffered []cliproxyexecutor.StreamChunk, remaining <-chan cliproxyexecutor.StreamChunk, aliasResult OAuthModelAliasResult, ephemeralResult bool, opts cliproxyexecutor.Options, cleanups ...func()) *cliproxyexecutor.StreamResult {
	out := make(chan cliproxyexecutor.StreamChunk)
	streamStart := time.Now()
	go func() {
		defer close(out)
		for _, cleanup := range cleanups {
			if cleanup != nil {
				defer cleanup()
			}
		}
		var failed bool
		forward := true
		var rewriter *StreamRewriter
		if aliasResult.ForceMapping && strings.TrimSpace(aliasResult.OriginalAlias) != "" {
			rewriter = NewStreamRewriter(StreamRewriteOptions{RewriteModel: aliasResult.OriginalAlias})
		}
		emit := func(chunk cliproxyexecutor.StreamChunk) bool {
			if chunk.Err != nil && !failed {
				failed = true
				entry := logEntryWithRequestID(ctx)
				warnLogUpstreamFailure(ctx, entry, provider, resultModel, auth, time.Since(streamStart), chunk.Err)
				rerr := resultErrorFromError(chunk.Err)
				action, okAction := matchRequestScopedErrorAction(auth, chunk.Err, m.runtimeConfigSnapshot())
				result := Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: false, Error: rerr, Options: opts}
				result.RetryAfter = retryAfterFromError(chunk.Err)
				result.CredentialScope = isCredentialScopedError(chunk.Err)
				applyRequestScopedActionToResult(action, okAction, &result)
				m.recordExecutionResult(ctx, result, auth, ephemeralResult)
			}
			if !forward {
				return false
			}
			if chunk.Err != nil {
				if ctx == nil {
					out <- chunk
					return true
				}
				select {
				case <-ctx.Done():
					forward = false
					return false
				case out <- chunk:
					return true
				}
			}
			if len(chunk.Payload) == 0 {
				return true
			}
			payload := rewriteForceMappedStreamChunk(rewriter, chunk.Payload)
			if len(payload) == 0 {
				return true
			}
			chunk.Payload = payload
			if ctx == nil {
				out <- chunk
				return true
			}
			select {
			case <-ctx.Done():
				forward = false
				return false
			case out <- chunk:
				return true
			}
		}
		for _, chunk := range buffered {
			if ok := emit(chunk); !ok {
				discardStreamChunks(remaining)
				return
			}
		}
		for chunk := range remaining {
			if ok := emit(chunk); !ok {
				discardStreamChunks(remaining)
				return
			}
		}
		if tail := finishForceMappedStreamChunks(rewriter); len(tail) > 0 {
			tailChunk := cliproxyexecutor.StreamChunk{Payload: tail}
			if !emit(tailChunk) {
				return
			}
		}
		if !failed && (ephemeralResult || claudeOAuthRequestCancellation(ctx, auth, nil) == nil) {
			m.recordExecutionResult(ctx, Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: true, Options: opts}, auth, ephemeralResult)
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: headers, Chunks: out}
}

func (m *Manager) executeStreamWithModelPool(ctx context.Context, executor ProviderExecutor, auth *Auth, provider string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options, routeModel, executionModel string, execModels []string, pooled bool, aliasResult OAuthModelAliasResult, routing *apiKeyModelRoutingSnapshot, allowRetry bool, ephemeralResult bool) (*cliproxyexecutor.StreamResult, error) {
	if executor == nil {
		return nil, &Error{Code: "executor_not_found", Message: "executor not registered"}
	}
	ctx = contextWithRequestedModelAlias(ctx, opts, routeModel)
	var lastErr error
	var upstreamErr error
	didRefreshOnUnauthorized := false
	for idx, execModel := range execModels {
		ctx = newUpstreamAttemptContext(ctx)
		ttftTimeout := m.streamFirstChunkTimeout(opts)
		resultModel := m.stateModelForExecution(auth, routeModel, execModel, pooled)
		execReq := req
		execReq.Model = execModel
		if executionModel != "" {
			execReq.Model = executionModel
		}
		execOpts := opts
		var errIntercept error
		execReq, execOpts, errIntercept = applyRequestAfterAuthInterceptor(ctx, executor, provider, execReq, execOpts, requestedModelAliasFromOptions(execOpts, routeModel))
		if errIntercept != nil {
			return nil, errIntercept
		}
		if executionModel == "" {
			execReq = attachResolvedAPIKeyModelInfo(routing, execReq, auth, routeModel, execModel)
		}
		if errCtx := ctx.Err(); errCtx != nil {
			return nil, errCtx
		}
		entry := logEntryWithRequestID(ctx)
		payload := execOpts.OriginalRequest
		if len(payload) == 0 {
			payload = execReq.Payload
		}
		execOpts.Metadata = ensureCanonicalSessionMetadata(execOpts.Metadata, execOpts.Headers, payload)
		ctx = syncMetadataSessionToContext(ctx, execOpts.Metadata)
		// Arm the TTFT scope only after local interception and request
		// preparation: the budget measures upstream responsiveness, so a slow
		// after-auth interceptor must not cancel the attempt before any
		// upstream request was even made.
		scope := newTTFTScope(ctx, ttftTimeout)
		attemptCtx := scope.ctx
		checkTTFTErr := func(err error) error {
			if t := scope.timeoutError(); t != nil {
				return t
			}
			return err
		}
		startStream := time.Now()
		streamResult, errStream := executor.ExecuteStream(attemptCtx, auth, execReq, execOpts)
		errStream = markUpstreamExecutionAttemptFromContext(ctx, errStream)
		if hasUpstreamExecutionAttempt(errStream) {
			upstreamErr = errStream
		}
		durationStream := time.Since(startStream)
		if errStream != nil {
			if errCtx := ctx.Err(); errCtx != nil {
				scope.release()
				return nil, errCtx
			}
			if allowRetry && !ephemeralResult {
				alreadyTried := didRefreshOnUnauthorized
				refreshed, okRefresh := m.tryRefreshAfterUnauthorized(newUpstreamAttemptContext(ctx), auth, errStream, alreadyTried)
				if okRefresh {
					if streamResult != nil {
						discardStreamChunks(streamResult.Chunks)
					}
					auth = refreshed
					publishSelectedAuthMetadata(execOpts.Metadata, auth)
					didRefreshOnUnauthorized = true
					ctx = newUpstreamAttemptContext(ctx)
					ctx = syncMetadataSessionToContext(ctx, execOpts.Metadata)
					scope.release()
					scope = newTTFTScope(ctx, ttftTimeout)
					attemptCtx = scope.ctx
					startRetry := time.Now()
					streamResult, errStream = executor.ExecuteStream(attemptCtx, auth, execReq, execOpts)
					errStream = markUpstreamExecutionAttemptFromContext(ctx, errStream)
					if hasUpstreamExecutionAttempt(errStream) {
						upstreamErr = errStream
					}
					durationRetry := time.Since(startRetry)
					errStream = checkTTFTErr(errStream)
					if errStream != nil {
						warnLogUpstreamFailure(ctx, entry, provider, execModel, auth, durationRetry, errStream)
						if errCtx := ctx.Err(); errCtx != nil {
							scope.release()
							if streamResult != nil {
								discardStreamChunks(streamResult.Chunks)
							}
							return nil, errCtx
						}
					}
				} else {
					warnLogUpstreamFailure(ctx, entry, provider, execModel, auth, durationStream, errStream)
				}
			} else {
				warnLogUpstreamFailure(ctx, entry, provider, execModel, auth, durationStream, errStream)
			}
		}
		if !ephemeralResult {
			if errCancel := claudeOAuthRequestCancellation(ctx, auth, errStream); errCancel != nil {
				scope.release()
				if streamResult != nil {
					discardStreamChunks(streamResult.Chunks)
				}
				return nil, errCancel
			}
		}
		streamResult, errStream = validateStreamResult(streamResult, errStream)
		errStream = markUpstreamExecutionAttemptFromContext(ctx, errStream)
		if errStream != nil {
			scope.release()
			if streamResult != nil {
				discardStreamChunks(streamResult.Chunks)
			}
			errStream = checkTTFTErr(errStream)
			rerr := resultErrorFromError(errStream)
			action, okAction := matchRequestScopedErrorAction(auth, errStream, m.runtimeConfigSnapshot())
			result := Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: false, Error: rerr, Options: execOpts}
			result.RetryAfter = retryAfterFromError(errStream)
			if isCredentialScopedError(errStream) {
				result.CredentialScope = true
			}
			applyRequestScopedActionToResult(action, okAction, &result)
			m.recordExecutionResult(ctx, result, auth, ephemeralResult)
			if okAction {
				if isRequestScopedStop(action, okAction) {
					return nil, wrapRequestStopError(errStream)
				}
				lastErr = errStream
				if result.CredentialScope {
					return nil, preferredExecutionAttemptError(errStream, upstreamErr)
				}
				continue
			}
			if isRequestInvalidError(errStream) {
				return nil, errStream
			}
			lastErr = errStream
			if result.CredentialScope {
				return nil, preferredExecutionAttemptError(errStream, upstreamErr)
			}
			continue
		}
		scope.stop()

		buffered, closed, bootstrapErr := readStreamBootstrap(attemptCtx, streamResult.Chunks)
		if bootstrapErr == nil && scope.timedOut() {
			bootstrapErr = scope.timeoutError()
		}
		bootstrapErr = markUpstreamExecutionAttemptFromContext(ctx, bootstrapErr)
		if hasUpstreamExecutionAttempt(bootstrapErr) {
			upstreamErr = newStreamBootstrapError(bootstrapErr, streamResult.Headers)
		}
		if bootstrapErr != nil {
			if errCtx := ctx.Err(); errCtx != nil {
				scope.release()
				discardStreamChunks(streamResult.Chunks)
				return nil, errCtx
			}
			bootstrapErr = checkTTFTErr(bootstrapErr)
			if allowRetry && !ephemeralResult {
				scope.stop()
				alreadyTried := didRefreshOnUnauthorized
				refreshed, okRefresh := m.tryRefreshAfterUnauthorized(newUpstreamAttemptContext(ctx), auth, bootstrapErr, alreadyTried)
				if okRefresh {
					discardStreamChunks(streamResult.Chunks)
					auth = refreshed
					publishSelectedAuthMetadata(execOpts.Metadata, auth)
					didRefreshOnUnauthorized = true
					ctx = newUpstreamAttemptContext(ctx)
					scope.release()
					scope = newTTFTScope(ctx, ttftTimeout)
					attemptCtx = scope.ctx
					startRetry := time.Now()
					retryStream, retryErr := executor.ExecuteStream(attemptCtx, auth, execReq, execOpts)
					retryErr = markUpstreamExecutionAttemptFromContext(ctx, retryErr)
					retryStream, retryErr = validateStreamResult(retryStream, retryErr)
					retryErr = markUpstreamExecutionAttemptFromContext(ctx, retryErr)
					scope.stop()
					retryErr = checkTTFTErr(retryErr)
					if retryErr != nil {
						if retryStream != nil {
							discardStreamChunks(retryStream.Chunks)
						}
						if errCtx := ctx.Err(); errCtx != nil {
							scope.release()
							return nil, errCtx
						}
						bootstrapErr = retryErr
						warnLogUpstreamFailure(ctx, entry, provider, execModel, auth, time.Since(startRetry), bootstrapErr)
						streamResult = &cliproxyexecutor.StreamResult{}
					} else {
						streamResult = retryStream
						buffered, closed, bootstrapErr = readStreamBootstrap(attemptCtx, streamResult.Chunks)
						if bootstrapErr == nil && scope.timedOut() {
							bootstrapErr = scope.timeoutError()
						}
						bootstrapErr = checkTTFTErr(bootstrapErr)
						bootstrapErr = markUpstreamExecutionAttemptFromContext(ctx, bootstrapErr)
						if bootstrapErr != nil {
							warnLogUpstreamFailure(ctx, entry, provider, execModel, auth, time.Since(startRetry), bootstrapErr)
						}
					}
				} else {
					warnLogUpstreamFailure(ctx, entry, provider, execModel, auth, time.Since(startStream), bootstrapErr)
				}
			} else {
				warnLogUpstreamFailure(ctx, entry, provider, execModel, auth, time.Since(startStream), bootstrapErr)
			}
			if hasUpstreamExecutionAttempt(bootstrapErr) {
				upstreamErr = newStreamBootstrapError(bootstrapErr, streamResult.Headers)
			}
		}
		if !ephemeralResult {
			if errCancel := claudeOAuthRequestCancellation(ctx, auth, bootstrapErr); errCancel != nil {
				scope.release()
				discardStreamChunks(streamResult.Chunks)
				return nil, errCancel
			}
		}
		if bootstrapErr != nil {
			scope.release()
			bootstrapErr = checkTTFTErr(bootstrapErr)
			action, okAction := matchRequestScopedErrorAction(auth, bootstrapErr, m.runtimeConfigSnapshot())
			if okAction {
				rerr := resultErrorFromError(bootstrapErr)
				result := Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: false, Error: rerr, Options: execOpts}
				result.RetryAfter = retryAfterFromError(bootstrapErr)
				if isCredentialScopedError(bootstrapErr) {
					result.CredentialScope = true
				}
				applyRequestScopedActionToResult(action, okAction, &result)
				m.recordExecutionResult(ctx, result, auth, ephemeralResult)
				discardStreamChunks(streamResult.Chunks)
				if isRequestScopedStop(action, okAction) {
					return nil, wrapRequestStopError(bootstrapErr)
				}
				lastErr = bootstrapErr
				if result.CredentialScope {
					currentErr := newStreamBootstrapError(bootstrapErr, streamResult.Headers)
					return nil, preferredExecutionAttemptError(currentErr, upstreamErr)
				}
				continue
			}
			if isRequestInvalidError(bootstrapErr) {
				rerr := resultErrorFromError(bootstrapErr)
				result := Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: false, Error: rerr, Options: execOpts}
				result.RetryAfter = retryAfterFromError(bootstrapErr)
				if isCredentialScopedError(bootstrapErr) {
					result.CredentialScope = true
				}
				m.recordExecutionResult(ctx, result, auth, ephemeralResult)
				discardStreamChunks(streamResult.Chunks)
				return nil, bootstrapErr
			}
			if idx < len(execModels)-1 {
				rerr := resultErrorFromError(bootstrapErr)
				result := Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: false, Error: rerr, Options: execOpts}
				result.RetryAfter = retryAfterFromError(bootstrapErr)
				if isCredentialScopedError(bootstrapErr) {
					result.CredentialScope = true
				}
				m.recordExecutionResult(ctx, result, auth, ephemeralResult)
				discardStreamChunks(streamResult.Chunks)
				lastErr = bootstrapErr
				if result.CredentialScope {
					currentErr := newStreamBootstrapError(bootstrapErr, streamResult.Headers)
					return nil, preferredExecutionAttemptError(currentErr, upstreamErr)
				}
				continue
			}
			rerr := resultErrorFromError(bootstrapErr)
			result := Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: false, Error: rerr, Options: execOpts}
			result.RetryAfter = retryAfterFromError(bootstrapErr)
			if isCredentialScopedError(bootstrapErr) {
				result.CredentialScope = true
			}
			m.recordExecutionResult(ctx, result, auth, ephemeralResult)
			discardStreamChunks(streamResult.Chunks)
			currentErr := newStreamBootstrapError(bootstrapErr, streamResult.Headers)
			return nil, preferredExecutionAttemptError(currentErr, upstreamErr)
		}

		payloadBytes := 0
		for _, chunk := range buffered {
			payloadBytes += len(chunk.Payload)
		}
		if closed && (payloadBytes == 0 || isEmptyCompletion(buffered)) {
			scope.release()
			var emptyErr error = errEmptyCompletion
			if payloadBytes == 0 {
				emptyErr = &Error{Code: "empty_stream", Message: "upstream stream closed before first payload", Retryable: true}
			}
			emptyErr = markUpstreamExecutionAttemptFromContext(ctx, emptyErr)
			currentErr := newStreamBootstrapError(emptyErr, streamResult.Headers)
			if hasUpstreamExecutionAttempt(emptyErr) {
				upstreamErr = currentErr
			}
			warnLogUpstreamFailure(ctx, entry, provider, execModel, auth, time.Since(startStream), emptyErr)
			result := Result{AuthID: auth.ID, Provider: provider, Model: resultModel, RouteModel: routeModel, Success: false, Error: resultErrorFromError(emptyErr), Options: execOpts}
			m.recordExecutionResult(ctx, result, auth, ephemeralResult)
			discardStreamChunks(streamResult.Chunks)
			if idx < len(execModels)-1 {
				lastErr = emptyErr
				continue
			}
			return nil, preferredExecutionAttemptError(currentErr, upstreamErr)
		}

		scope.commit()
		remaining := streamResult.Chunks
		if closed {
			discardStreamChunks(streamResult.Chunks)
			closedCh := make(chan cliproxyexecutor.StreamChunk)
			close(closedCh)
			remaining = closedCh
		}
		attemptAliasResult := resolveAttemptAliasResult(routing, auth, routeModel, execModel, aliasResult)
		return m.wrapStreamResult(ctx, auth.Clone(), provider, resultModel, routeModel, streamResult.Headers, buffered, remaining, attemptAliasResult, ephemeralResult, execOpts, scope.release), nil
	}
	if lastErr == nil {
		lastErr = &Error{Code: "auth_not_found", Message: "no upstream model available"}
	}
	return nil, preferredExecutionAttemptError(lastErr, upstreamErr)
}
