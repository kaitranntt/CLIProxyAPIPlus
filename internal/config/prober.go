package config

import "time"

const (
	defaultCredentialProberInterval       = 60 * time.Second
	defaultCredentialProberTimeout        = 10 * time.Second
	defaultCredentialProberMaxConcurrency = 4
	defaultCredentialProberRatePerMinute  = 60
	defaultCredentialProberBackoffBase    = 5 * time.Second
	defaultCredentialProberBackoffMax     = 5 * time.Minute
	defaultCredentialProberPath           = "/v1/models"
)

// CredentialProberConfig controls optional active health probing for registered credentials.
// When enabled, the conductor periodically issues a lightweight HTTP probe per credential
// and feeds failures into the existing cooldown/suspension machinery.
type CredentialProberConfig struct {
	// Enabled turns active credential health probing on. Default false.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Interval is the period between probe sweeps. Default 60s.
	Interval time.Duration `yaml:"interval" json:"interval"`
	// Timeout is the maximum duration a single probe request may take. Default 10s.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
	// MaxConcurrency limits the number of in-flight probes. Default 4.
	MaxConcurrency int `yaml:"max-concurrency" json:"max-concurrency"`
	// RateLimitPerMinute caps the number of probe requests across all credentials per minute. Default 60.
	RateLimitPerMinute int `yaml:"rate-limit-per-minute" json:"rate-limit-per-minute"`
	// BackoffBase is the initial cooldown applied when a probe fails. Default 5s.
	BackoffBase time.Duration `yaml:"backoff-base" json:"backoff-base"`
	// BackoffMax is the maximum probe-induced cooldown. Default 5m.
	BackoffMax time.Duration `yaml:"backoff-max" json:"backoff-max"`
	// DefaultProbePath is the HTTP path appended to the credential base_url for the probe. Default /v1/models.
	DefaultProbePath string `yaml:"default-probe-path" json:"default-probe-path"`
}

// DefaultCredentialProberConfig returns the prober default configuration.
func DefaultCredentialProberConfig() CredentialProberConfig {
	return CredentialProberConfig{
		Enabled:            false,
		Interval:           defaultCredentialProberInterval,
		Timeout:            defaultCredentialProberTimeout,
		MaxConcurrency:     defaultCredentialProberMaxConcurrency,
		RateLimitPerMinute: defaultCredentialProberRatePerMinute,
		BackoffBase:        defaultCredentialProberBackoffBase,
		BackoffMax:         defaultCredentialProberBackoffMax,
		DefaultProbePath:   defaultCredentialProberPath,
	}
}
