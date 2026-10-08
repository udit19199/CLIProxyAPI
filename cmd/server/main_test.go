package main

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestArgvEnablesBoolFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		flag string
		want bool
	}{
		{name: "bare long flag", args: []string{"--discover-json"}, flag: "discover-json", want: true},
		{name: "assigned true", args: []string{"--discover-json=true"}, flag: "discover-json", want: true},
		{name: "assigned false", args: []string{"--discover-json=false"}, flag: "discover-json", want: false},
		{name: "does not match timeout", args: []string{"--discover-timeout", "3"}, flag: "discover", want: false},
		{name: "bare discover", args: []string{"--discover"}, flag: "discover", want: true},
		{name: "stops at terminator", args: []string{"--", "--discover-json"}, flag: "discover-json", want: false},
		{name: "stops at non-flag", args: []string{"foo", "--discover-json"}, flag: "discover-json", want: false},
		{name: "skips config value", args: []string{"--config", "config.yaml", "--discover-json"}, flag: "discover-json", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := argvEnablesBoolFlag(tt.args, tt.flag); got != tt.want {
				t.Fatalf("argvEnablesBoolFlag(%v, %q) = %t, want %t", tt.args, tt.flag, got, tt.want)
			}
		})
	}
}

func TestShouldEnableExampleAPIKeySafeMode(t *testing.T) {
	cfgWithExampleKey := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"real-key", " your-api-key-1 "},
		},
	}
	cfgWithRealKey := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"real-key"},
		},
	}

	tests := []struct {
		name               string
		cfg                *config.Config
		commandMode        bool
		cloudConfigMissing bool
		want               bool
	}{
		{
			name: "normal server with example key",
			cfg:  cfgWithExampleKey,
			want: true,
		},
		{
			name:        "one-shot command is not blocked",
			cfg:         cfgWithExampleKey,
			commandMode: true,
			want:        false,
		},
		{
			name:               "cloud standby without config is not blocked",
			cfg:                cfgWithExampleKey,
			cloudConfigMissing: true,
			want:               false,
		},
		{
			name: "normal server with real key",
			cfg:  cfgWithRealKey,
			want: false,
		},
		{
			name: "nil config",
			cfg:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldEnableExampleAPIKeySafeMode(tt.cfg, tt.commandMode, tt.cloudConfigMissing)
			if got != tt.want {
				t.Fatalf("shouldEnableExampleAPIKeySafeMode() = %t, want %t", got, tt.want)
			}
		})
	}
}
