package antigravity

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBuildAuthURLUsesDefaultClientIDWhenEnvUnset(t *testing.T) {
	t.Setenv(ClientIDEnv, "")

	auth := NewAntigravityAuth(nil, &http.Client{})
	rawURL := auth.BuildAuthURL("state", "http://localhost:51121/oauth-callback")
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}

	if got := parsed.Query().Get("client_id"); got != DefaultClientID {
		t.Fatalf("client_id = %q, want default", got)
	}
}

func TestBuildAuthURLUsesEnvClientID(t *testing.T) {
	t.Setenv(ClientIDEnv, "env-client-id")

	auth := NewAntigravityAuth(nil, &http.Client{})
	rawURL := auth.BuildAuthURL("state", "http://localhost:51121/oauth-callback")
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}

	if got := parsed.Query().Get("client_id"); got != "env-client-id" {
		t.Fatalf("client_id = %q, want env override", got)
	}
}

func TestExchangeCodeForTokensUsesDefaultOAuthCredentialsWhenEnvUnset(t *testing.T) {
	t.Setenv(ClientIDEnv, "")
	t.Setenv(ClientSecretEnv, "")

	auth := NewAntigravityAuth(nil, &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != TokenEndpoint {
			t.Fatalf("token URL = %s, want %s", req.URL.String(), TokenEndpoint)
		}
		if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q", got)
		}
		body, errRead := io.ReadAll(req.Body)
		if errRead != nil {
			t.Fatalf("read body: %v", errRead)
		}
		values, errParse := url.ParseQuery(string(body))
		if errParse != nil {
			t.Fatalf("parse form: %v", errParse)
		}
		if got := values.Get("client_id"); got != DefaultClientID {
			t.Fatalf("client_id = %q, want default", got)
		}
		if got := values.Get("client_secret"); got != DefaultClientSecret {
			t.Fatalf("client_secret = %q, want default", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`)),
		}, nil
	})})

	token, err := auth.ExchangeCodeForTokens(context.Background(), "code", "http://localhost:51121/oauth-callback")
	if err != nil {
		t.Fatalf("ExchangeCodeForTokens error: %v", err)
	}
	if token.AccessToken != "access" || token.RefreshToken != "refresh" {
		t.Fatalf("unexpected token response: %+v", token)
	}
}

func TestExchangeCodeForTokensUsesEnvOAuthCredentials(t *testing.T) {
	t.Setenv(ClientIDEnv, "env-client-id")
	t.Setenv(ClientSecretEnv, "env-client-secret")

	auth := NewAntigravityAuth(nil, &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body, errRead := io.ReadAll(req.Body)
		if errRead != nil {
			t.Fatalf("read body: %v", errRead)
		}
		values, errParse := url.ParseQuery(string(body))
		if errParse != nil {
			t.Fatalf("parse form: %v", errParse)
		}
		if got := values.Get("client_id"); got != "env-client-id" {
			t.Fatalf("client_id = %q, want env override", got)
		}
		if got := values.Get("client_secret"); got != "env-client-secret" {
			t.Fatalf("client_secret = %q, want env override", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`)),
		}, nil
	})})

	if _, err := auth.ExchangeCodeForTokens(context.Background(), "code", "http://localhost:51121/oauth-callback"); err != nil {
		t.Fatalf("ExchangeCodeForTokens error: %v", err)
	}
}
