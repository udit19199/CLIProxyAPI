package util

import (
	"runtime"
	"testing"
)

func TestResolveGitHubToken(t *testing.T) {
	tests := []struct {
		name        string
		githubToken string
		lowerToken  string
		want        string
	}{
		{
			name:        "GITHUB_TOKEN has highest priority",
			githubToken: " primary-token ",
			lowerToken:  "lower-token",
			want:        "primary-token",
		},
		{
			name:        "lowercase token is second priority",
			githubToken: " ",
			lowerToken:  " lower-token ",
			want:        "lower-token",
		},
		{
			name: "no token configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && tt.name == "GITHUB_TOKEN has highest priority" {
				t.Skip("environment variables are case-insensitive on Windows")
			}
			if tt.githubToken != "" {
				t.Setenv("GITHUB_TOKEN", tt.githubToken)
			}
			if tt.lowerToken != "" {
				t.Setenv("github_token", tt.lowerToken)
			}
			if tt.githubToken == "" && tt.lowerToken == "" {
				t.Setenv("GITHUB_TOKEN", "")
				t.Setenv("github_token", "")
			}

			if got := ResolveGitHubToken(); got != tt.want {
				t.Fatalf("ResolveGitHubToken() = %q, want %q", got, tt.want)
			}
		})
	}
}
