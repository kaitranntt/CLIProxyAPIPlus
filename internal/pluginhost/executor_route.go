package pluginhost

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/homeplugins"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// HomeLocalExecutorFence captures the host snapshot before asking Home for a grant.
func (h *Host) HomeLocalExecutorFence() func() bool {
	snapshot := h.Snapshot()
	return func() bool { return snapshot.enabled && h.Snapshot() == snapshot }
}

// executorPluginReady reports whether the named plugin can actually execute a
// request right now: it must declare an executor capability AND resolve a
// non-empty provider identifier, allow static execution without
// selected auth, and declare formats compatible with the current request.
// Routing pre-checks use this so that targets which would fail at execution are
// treated as unhandled and fall through to lower-priority routers instead of
// returning handled then 500ing.
func (h *Host) executorPluginReady(pluginID string, routeReq pluginapi.ModelRouteRequest) bool {
	if h == nil {
		return false
	}
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return false
	}
	for _, record := range h.activeRecords() {
		if record.id != pluginID || h.isPluginFused(record.id) {
			continue
		}
		executor := record.plugin.Capabilities.Executor
		if executor == nil {
			return false
		}
		if !executorScopeAllowsStaticModels(record.plugin.Capabilities) {
			return false
		}
		provider, okProvider := h.executorProvider(record, executor)
		if !okProvider {
			return false
		}
		adapter := newExecutorAdapterRegistration(h, record, provider, executor).adapter
		return adapter.supportsExecutorFormats(
			coreexecutor.Request{Model: routeReq.RequestedModel, Payload: routeReq.Body},
			coreexecutor.Options{
				Stream:          routeReq.Stream,
				OriginalRequest: routeReq.Body,
				SourceFormat:    sdktranslator.FromString(routeReq.SourceFormat),
				ResponseFormat:  sdktranslator.FromString(routeReq.SourceFormat),
				Headers:         cloneHeader(routeReq.Headers),
				Query:           cloneValues(routeReq.Query),
				Metadata:        cloneInterceptorMetadata(routeReq.Metadata),
			},
		)
	}
	return false
}

func (a *executorAdapter) supportsExecutorFormats(req coreexecutor.Request, opts coreexecutor.Options) bool {
	if a == nil {
		return false
	}
	inputRequested := executorInputFormat(req, opts)
	requestedFormat := executorRequestedFormat(req, opts)
	inputFormat, errInput := a.selectExecutorInputFormat(inputRequested)
	if errInput != nil {
		return false
	}
	_, errOutput := a.selectExecutorOutputFormat(requestedFormat, inputFormat)
	return errOutput == nil
}

// PluginExecutorRequestToFormat reports the executor input format selected for a direct plugin executor route.
func (h *Host) PluginExecutorRequestToFormat(pluginID string, req coreexecutor.Request, opts coreexecutor.Options) sdktranslator.Format {
	adapter, errAdapter := h.executorAdapterForPlugin(nil, pluginID, "")
	if errAdapter != nil {
		return ""
	}
	return adapter.RequestToFormat(req, opts)
}

// ExecutePluginExecutor executes a request with the named plugin executor without changing the requested model.
func (h *Host) ExecutePluginExecutor(ctx context.Context, pluginID string, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	resp, _, err := h.ExecutePluginExecutorWithAttribution(ctx, pluginID, req, opts)
	return resp, err
}

// ExecutePluginExecutorWithAttribution executes a request with the named plugin executor and returns optional plugin attribution.
func (h *Host) ExecutePluginExecutorWithAttribution(ctx context.Context, pluginID string, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, *pluginapi.UsageAttribution, error) {
	adapter, errAdapter := h.executorAdapterForPlugin(ctx, pluginID, "")
	if errAdapter != nil {
		return coreexecutor.Response{}, nil, errAdapter
	}
	resp, attr, err := adapter.executeWithAttribution(ctx, (*coreauth.Auth)(nil), req, opts)
	if err != nil {
		return coreexecutor.Response{}, attr, err
	}
	if coreauth.IsEmptyCompletionPayload(resp.Payload) {
		return coreexecutor.Response{}, attr, coreauth.EmptyCompletionError()
	}
	return resp, attr, nil
}

// ExecutePluginExecutorStream executes a streaming request with the named plugin executor without changing the requested model.
func (h *Host) ExecutePluginExecutorStream(ctx context.Context, pluginID string, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	result, _, err := h.ExecutePluginExecutorStreamWithAttribution(ctx, pluginID, req, opts)
	return result, err
}

// ExecutePluginExecutorStreamWithAttribution executes a streaming request with the named plugin executor and returns optional plugin attribution.
func (h *Host) ExecutePluginExecutorStreamWithAttribution(ctx context.Context, pluginID string, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, *pluginapi.UsageAttribution, error) {
	adapter, errAdapter := h.executorAdapterForPlugin(ctx, pluginID, "")
	if errAdapter != nil {
		return nil, nil, errAdapter
	}
	streamResult, attr, err := adapter.executeStreamWithAttribution(ctx, (*coreauth.Auth)(nil), req, opts)
	if err != nil {
		return nil, attr, err
	}
	return wrapStreamEmptyCompletion(ctx, streamResult), attr, nil
}

// wrapStreamEmptyCompletion wraps a plugin stream so that a terminal but empty
// completion (no content, no tool calls) surfaces as an empty-completion error
// instead of a clean stream end, mirroring the conductor's aggregate-at-close
// judgment. Recognized protocol framing is buffered only until meaningful output
// appears or the stream closes; unrecognized streams remain pass-through.
func wrapStreamEmptyCompletion(ctx context.Context, streamResult *coreexecutor.StreamResult) *coreexecutor.StreamResult {
	if streamResult == nil || streamResult.Chunks == nil {
		return streamResult
	}
	if ctx == nil {
		ctx = context.Background()
	}
	src := streamResult.Chunks
	wrapped := make(chan coreexecutor.StreamChunk)
	go func() {
		defer close(wrapped)
		buffered := make([]coreexecutor.StreamChunk, 0, 1)
		var detector coreauth.StreamBootstrapDetector
		forwarding := false
		forward := func(chunk coreexecutor.StreamChunk) bool {
			select {
			case <-ctx.Done():
				return false
			case wrapped <- chunk:
				return true
			}
		}
		flush := func() bool {
			for _, chunk := range buffered {
				if !forward(chunk) {
					return false
				}
			}
			buffered = nil
			return true
		}

		for {
			var (
				chunk coreexecutor.StreamChunk
				ok    bool
			)
			select {
			case <-ctx.Done():
				return
			case chunk, ok = <-src:
			}
			if !ok {
				if !forwarding {
					payloadBytes := 0
					for _, c := range buffered {
						payloadBytes += len(c.Payload)
					}
					if payloadBytes == 0 {
						// Zero-payload chunks are dropped downstream; a stream of only
						// such chunks is an empty stream, not a successful completion.
						_ = forward(coreexecutor.StreamChunk{Err: &coreauth.Error{
							Code:      "empty_stream",
							Message:   "upstream stream closed before first payload",
							Retryable: true,
						}})
						return
					}
					// Judge with the incremental detector state instead of re-parsing
					// the concatenated payload: separately chunked SSE frames do not
					// reassemble into valid input for the payload-level check.
					if detector.Finish() {
						_ = forward(coreexecutor.StreamChunk{Err: coreauth.EmptyCompletionError()})
						return
					}
				}
				_ = flush()
				return
			}
			if forwarding {
				if !forward(chunk) {
					return
				}
				if chunk.Err != nil {
					return
				}
				continue
			}

			buffered = append(buffered, chunk)
			if chunk.Err != nil {
				// Before any semantic output, protocol framing is not client-visible.
				// Surface the upstream failure first so the HTTP layer can still
				// choose an error response instead of committing a successful stream.
				buffered = buffered[:0]
				forwarding = true
				if !forward(chunk) {
					return
				}
				return
			}
			if detector.Observe(chunk.Payload) {
				forwarding = true
				if !flush() {
					return
				}
			}
			if detector.IsTerminalEmpty() {
				discardStreamChunks(src)
				_ = forward(coreexecutor.StreamChunk{Err: coreauth.EmptyCompletionError()})
				return
			}
		}
	}()
	return &coreexecutor.StreamResult{Chunks: wrapped, Headers: streamResult.Headers}
}

func discardStreamChunks(ch <-chan coreexecutor.StreamChunk) {
	if ch == nil {
		return
	}
	go func() {
		for range ch {
		}
	}()
}

func streamChunkPayload(chunks []coreexecutor.StreamChunk) []byte {
	var payload []byte
	for _, chunk := range chunks {
		payload = append(payload, chunk.Payload...)
	}
	return payload
}

// CountPluginExecutor executes a count-tokens request with the named plugin executor without changing the requested model.
func (h *Host) CountPluginExecutor(ctx context.Context, pluginID string, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	adapter, errAdapter := h.executorAdapterForPlugin(ctx, pluginID, "")
	if errAdapter != nil {
		return coreexecutor.Response{}, errAdapter
	}
	return adapter.CountTokens(ctx, (*coreauth.Auth)(nil), req, opts)
}

// HomeExecutorPlugins returns the live executor plugins and their allowed Home credential providers.
func (h *Host) HomeExecutorPlugins() map[string][]string {
	ready := make(map[string][]string)
	snapshot := h.Snapshot()
	if !snapshot.enabled {
		return ready
	}
	for _, record := range h.activeRecordsFromSnapshot(snapshot) {
		var providers []string
		for _, provider := range record.plugin.Capabilities.ExecutorAuthProviders {
			provider = strings.ToLower(strings.TrimSpace(provider))
			if provider != "" && !slices.Contains(providers, provider) {
				providers = append(providers, provider)
			}
		}
		if len(providers) == 0 {
			continue
		}
		if _, errAdapter := h.executorAdapterForRecord(record, providers[0]); errAdapter == nil {
			slices.Sort(providers)
			ready[record.id] = providers
		}
	}
	return ready
}

// ResolveHomePluginExecutor binds a ready plugin executor to the selected credential provider.
func (h *Host) ResolveHomePluginExecutor(pluginID, authProvider string) (coreauth.ProviderExecutor, error) {
	authProvider = strings.ToLower(strings.TrimSpace(authProvider))
	if authProvider == "" {
		return nil, fmt.Errorf("Home credential provider is required for plugin executor %q", pluginID)
	}
	adapter, errAdapter := h.executorAdapterForPlugin(nil, pluginID, authProvider)
	if errAdapter != nil {
		return nil, errAdapter
	}
	return adapter, nil
}

func (h *Host) executorAdapterForPlugin(ctx context.Context, pluginID, authProvider string) (*executorAdapter, error) {
	grant, local := homeplugins.ExecutorGrant(ctx)
	if h == nil {
		return nil, fmt.Errorf("plugin host is unavailable")
	}
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return nil, fmt.Errorf("target executor plugin id is required")
	}
	snapshot := h.Snapshot()
	if !local && authProvider != "" && !snapshot.enabled {
		return nil, fmt.Errorf("plugin executor %q is disabled", pluginID)
	}
	for _, record := range h.activeRecordsFromSnapshot(snapshot) {
		if record.id != pluginID {
			continue
		}
		if local && (grant.Plugin != pluginID || grant.Version != record.version || !grant.Valid() ||
			!executorScopeAllowsStaticModels(record.plugin.Capabilities)) {
			break
		}
		adapter, errAdapter := h.executorAdapterForRecord(record, authProvider)
		if local && (errAdapter != nil || len(adapter.inputFormats) == 0 || len(adapter.outputFormats) == 0) {
			break
		}
		if local {
			adapter.homePlugin = record.loaded
		}
		return adapter, errAdapter
	}
	if local {
		return nil, &coreauth.Error{
			Code:       coreauth.ErrorCodeHomeExecutorPluginUnavailable,
			Message:    fmt.Sprintf("plugin executor %q is unavailable", pluginID),
			HTTPStatus: http.StatusServiceUnavailable,
		}
	}
	return nil, fmt.Errorf("plugin executor %q is missing or disabled", pluginID)
}

func (h *Host) executorAdapterForRecord(record capabilityRecord, authProvider string) (*executorAdapter, error) {
	if h.isPluginFused(record.id) {
		return nil, fmt.Errorf("plugin executor %q is fused", record.id)
	}
	caps := record.plugin.Capabilities
	if caps.Executor == nil {
		return nil, fmt.Errorf("plugin %q does not declare an executor", record.id)
	}
	if authProvider != "" && !supportsExecutorAuthProvider(caps, authProvider) {
		return nil, fmt.Errorf("plugin executor %q does not declare support for Home credential provider %q", record.id, authProvider)
	}
	provider, okProvider := h.executorProvider(record, caps.Executor)
	if !okProvider {
		return nil, fmt.Errorf("plugin executor %q has no provider identifier", record.id)
	}
	adapter := newExecutorAdapterRegistration(h, record, provider, caps.Executor).adapter
	if authProvider != "" {
		if len(adapter.inputFormats) == 0 || len(adapter.outputFormats) == 0 {
			return nil, fmt.Errorf("plugin executor %q has incomplete protocol declarations", record.id)
		}
		if !h.homeExecutorAvailable(record.id, record.loaded, authProvider) {
			return nil, fmt.Errorf("plugin executor %q is not ready for Home dispatch", record.id)
		}
		adapter.homeAuthProvider = authProvider
		adapter.homePlugin = record.loaded
	}
	return adapter, nil
}

func (h *Host) homeExecutorAvailable(id string, loaded *loadedPlugin, authProvider string) bool {
	if h == nil || loaded == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	snapshot := h.Snapshot()
	if !snapshot.enabled || h.loaded[id] != loaded || loaded.homeSuspended {
		return false
	}
	if _, fused := h.fused[id]; fused {
		return false
	}
	// Reconfiguration can change allowed providers without replacing the instance.
	for _, record := range snapshot.records {
		if record.id == id && record.loaded == loaded {
			caps := record.plugin.Capabilities
			return caps.Executor != nil && supportsExecutorAuthProvider(caps, authProvider)
		}
	}
	return false
}

func supportsExecutorAuthProvider(caps pluginapi.Capabilities, provider string) bool {
	return slices.ContainsFunc(caps.ExecutorAuthProviders, func(declared string) bool {
		return strings.EqualFold(strings.TrimSpace(declared), provider)
	})
}
