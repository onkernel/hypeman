package images

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeImageName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		// Valid images with full reference
		{"docker.io/library/alpine:latest", "docker.io/library/alpine:latest", false},
		{"ghcr.io/myorg/myapp:v1.0.0", "ghcr.io/myorg/myapp:v1.0.0", false},
		
		// Shorthand (gets expanded)
		{"alpine", "docker.io/library/alpine:latest", false},
		{"alpine:3.18", "docker.io/library/alpine:3.18", false},
		{"nginx", "docker.io/library/nginx:latest", false},
		{"nginx:alpine", "docker.io/library/nginx:alpine", false},
		
		// Without tag (gets :latest added)
		{"docker.io/library/alpine", "docker.io/library/alpine:latest", false},
		{"ubuntu", "docker.io/library/ubuntu:latest", false},
		
		// Invalid
		{"", "", true},
		{"invalid::", "", true},
		{"has spaces", "", true},
		{"UPPERCASE", "", true}, // Repository names must be lowercase
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := normalizeImageName(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

