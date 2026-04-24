// Package antigravity provides OAuth2 authentication functionality for the Antigravity provider.
package antigravity

import (
	"os"
	"strings"
)

// OAuth client credentials and configuration
const (
	ClientIDEnv     = "CLIPROXY_ANTIGRAVITY_OAUTH_CLIENT_ID"
	ClientSecretEnv = "CLIPROXY_ANTIGRAVITY_OAUTH_CLIENT_SECRET"
	CallbackPort    = 51121
)

var (
	ClientID     = strings.TrimSpace(os.Getenv(ClientIDEnv))
	ClientSecret = strings.TrimSpace(os.Getenv(ClientSecretEnv))
)

// Scopes defines the OAuth scopes required for Antigravity authentication
var Scopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

// OAuth2 endpoints for Google authentication
const (
	TokenEndpoint    = "https://oauth2.googleapis.com/token"
	AuthEndpoint     = "https://accounts.google.com/o/oauth2/v2/auth"
	UserInfoEndpoint = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
)

// Antigravity API configuration
const (
	APIEndpoint    = "https://cloudcode-pa.googleapis.com"
	APIVersion     = "v1internal"
	APIUserAgent   = "google-api-nodejs-client/9.15.1"
	APIClient      = "google-cloud-sdk vscode_cloudshelleditor/0.1"
	ClientMetadata = `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`
)
