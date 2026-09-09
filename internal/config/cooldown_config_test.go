package config

import "testing"

func TestCooldownConfigDefaults(t *testing.T) {
	data := []byte(`
host: "127.0.0.1"
port: 8080
`)
	cfg, err := ParseConfigBytes(data)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.TransientErrorCooldownSeconds != 0 {
		t.Fatalf("TransientErrorCooldownSeconds default = %d, want 0", cfg.TransientErrorCooldownSeconds)
	}
	if cfg.QuotaCooldownFloorSeconds != 1 {
		t.Fatalf("QuotaCooldownFloorSeconds default = %d, want 1", cfg.QuotaCooldownFloorSeconds)
	}
	if cfg.TransientCooldownByStatus != nil {
		t.Fatalf("TransientCooldownByStatus default = %v, want nil", cfg.TransientCooldownByStatus)
	}
}

func TestCooldownConfigParse(t *testing.T) {
	data := []byte(`
host: "127.0.0.1"
port: 8080
transient-error-cooldown-seconds: 10
quota-cooldown-floor-seconds: 5
transient-cooldown-by-status:
  - status: 408
    cooldown-seconds: 2
  - status: 503
    cooldown-seconds: 15
`)
	cfg, err := ParseConfigBytes(data)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.TransientErrorCooldownSeconds != 10 {
		t.Fatalf("TransientErrorCooldownSeconds = %d, want 10", cfg.TransientErrorCooldownSeconds)
	}
	if cfg.QuotaCooldownFloorSeconds != 5 {
		t.Fatalf("QuotaCooldownFloorSeconds = %d, want 5", cfg.QuotaCooldownFloorSeconds)
	}
	if len(cfg.TransientCooldownByStatus) != 2 {
		t.Fatalf("TransientCooldownByStatus len = %d, want 2", len(cfg.TransientCooldownByStatus))
	}
	found := map[int]int{}
	for _, r := range cfg.TransientCooldownByStatus {
		found[r.Status] = r.CooldownSeconds
	}
	if found[408] != 2 {
		t.Fatalf("status 408 cooldown = %d, want 2", found[408])
	}
	if found[503] != 15 {
		t.Fatalf("status 503 cooldown = %d, want 15", found[503])
	}
}
