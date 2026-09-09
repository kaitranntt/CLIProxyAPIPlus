package api

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestTransientCooldownByStatusEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []config.TransientCooldownByStatusRule
		b    []config.TransientCooldownByStatusRule
		want bool
	}{
		{
			name: "identical",
			a:    []config.TransientCooldownByStatusRule{{Status: 408, CooldownSeconds: 2}, {Status: 503, CooldownSeconds: 10}},
			b:    []config.TransientCooldownByStatusRule{{Status: 408, CooldownSeconds: 2}, {Status: 503, CooldownSeconds: 10}},
			want: true,
		},
		{
			name: "different values",
			a:    []config.TransientCooldownByStatusRule{{Status: 408, CooldownSeconds: 2}},
			b:    []config.TransientCooldownByStatusRule{{Status: 408, CooldownSeconds: 5}},
			want: false,
		},
		{
			name: "new drops a status",
			a:    []config.TransientCooldownByStatusRule{{Status: 408, CooldownSeconds: 2}, {Status: 503, CooldownSeconds: 10}},
			b:    []config.TransientCooldownByStatusRule{{Status: 408, CooldownSeconds: 2}},
			want: false,
		},
		{
			name: "new duplicates a status",
			a:    []config.TransientCooldownByStatusRule{{Status: 408, CooldownSeconds: 2}, {Status: 503, CooldownSeconds: 10}},
			b:    []config.TransientCooldownByStatusRule{{Status: 408, CooldownSeconds: 2}, {Status: 408, CooldownSeconds: 2}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := transientCooldownByStatusEqual(tt.a, tt.b); got != tt.want {
				t.Fatalf("transientCooldownByStatusEqual(%+v, %+v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
