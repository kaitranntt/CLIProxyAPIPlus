package openai

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

// runResponsesWebsocketForward drives forwardResponsesWebsocket over a real
// websocket pair with the given payloads / executor error and reports the
// ErrorMessage the request logger stored in API_RESPONSE_ERROR.
func runResponsesWebsocketForward(t *testing.T, payloads []string, upstreamErr *interfaces.ErrorMessage) (string, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	type forwardResult struct {
		logged string
		exists bool
	}
	resultCh := make(chan forwardResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := responsesWebsocketUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = r

		data := make(chan []byte, len(payloads))
		for _, payload := range payloads {
			data <- []byte(payload)
		}
		close(data)
		errCh := make(chan *interfaces.ErrorMessage, 1)
		if upstreamErr != nil {
			errCh <- upstreamErr
		}
		close(errCh)

		h := NewOpenAIResponsesAPIHandler(handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{RequestLog: true}, nil))
		_, _, _, _, _ = h.forwardResponsesWebsocket(
			ctx,
			newResponsesWebsocketWriter(conn),
			func(...interface{}) {},
			data,
			errCh,
			newInMemoryWebsocketTimelineLog(),
			"session-1",
		)
		res := forwardResult{}
		if value, exists := ctx.Get("API_RESPONSE_ERROR"); exists {
			if errs, ok := value.([]*interfaces.ErrorMessage); ok && len(errs) > 0 && errs[0] != nil && errs[0].Error != nil {
				res.exists = true
				res.logged = errs[0].Error.Error()
			}
		}
		resultCh <- res
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	select {
	case res := <-resultCh:
		return res.logged, res.exists
	case <-time.After(5 * time.Second):
		t.Fatal("forwarder did not finish")
		return "", false
	}
}

// An executor error delivered over the errs channel can carry the raw
// upstream body, which may echo the credential we sent upstream. The request
// logger records whatever reaches LoggingAPIResponseError verbatim, so the
// websocket forwarder must sanitize before logging.
func TestForwardResponsesWebsocketSanitizesLoggedUpstreamError(t *testing.T) {
	const secret = "sk-forward-errs-secret"
	logged, exists := runResponsesWebsocketForward(t,
		[]string{`{"type":"response.output_text.delta","delta":"hi"}`},
		&interfaces.ErrorMessage{StatusCode: http.StatusUnauthorized, Error: errors.New("upstream rejected request: Authorization: Bearer " + secret)},
	)
	if !exists {
		t.Fatal("expected the forwarder to record the upstream error in API_RESPONSE_ERROR")
	}
	if strings.Contains(logged, secret) {
		t.Fatalf("request log stored the credential verbatim: %q", logged)
	}
}

// Same boundary for the upstream "error" event payload: the terminal payload
// error is logged before the sanitized rebuild happens, so the log copy must
// be sanitized on its own.
func TestForwardResponsesWebsocketSanitizesLoggedErrorPayload(t *testing.T) {
	const secret = "sk-forward-payload-secret"
	logged, exists := runResponsesWebsocketForward(t,
		[]string{`{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"bad request: Authorization: Bearer ` + secret + `"}}`},
		nil,
	)
	if !exists {
		t.Fatal("expected the forwarder to record the upstream error payload in API_RESPONSE_ERROR")
	}
	if strings.Contains(logged, secret) {
		t.Fatalf("request log stored the credential verbatim: %q", logged)
	}
}
