package qemu

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQMPCommand_Marshal(t *testing.T) {
	tests := []struct {
		name     string
		cmd      qmpCommand
		expected string
	}{
		{
			name:     "stop command",
			cmd:      qmpCommand{Execute: "stop"},
			expected: `{"execute":"stop"}`,
		},
		{
			name:     "cont command",
			cmd:      qmpCommand{Execute: "cont"},
			expected: `{"execute":"cont"}`,
		},
		{
			name:     "query-status command",
			cmd:      qmpCommand{Execute: "query-status"},
			expected: `{"execute":"query-status"}`,
		},
		{
			name:     "quit command",
			cmd:      qmpCommand{Execute: "quit"},
			expected: `{"execute":"quit"}`,
		},
		{
			name:     "system_powerdown command",
			cmd:      qmpCommand{Execute: "system_powerdown"},
			expected: `{"execute":"system_powerdown"}`,
		},
		{
			name:     "system_reset command",
			cmd:      qmpCommand{Execute: "system_reset"},
			expected: `{"execute":"system_reset"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.cmd)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

func TestQMPStatusResponse_Unmarshal(t *testing.T) {
	tests := []struct {
		name            string
		json            string
		expectedStatus  string
		expectedRunning bool
	}{
		{
			name:            "running",
			json:            `{"return":{"running":true,"status":"running"}}`,
			expectedStatus:  "running",
			expectedRunning: true,
		},
		{
			name:            "paused",
			json:            `{"return":{"running":false,"status":"paused"}}`,
			expectedStatus:  "paused",
			expectedRunning: false,
		},
		{
			name:            "shutdown",
			json:            `{"return":{"running":false,"status":"shutdown"}}`,
			expectedStatus:  "shutdown",
			expectedRunning: false,
		},
		{
			name:            "prelaunch",
			json:            `{"return":{"running":false,"status":"prelaunch"}}`,
			expectedStatus:  "prelaunch",
			expectedRunning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp qmpStatusResponse
			err := json.Unmarshal([]byte(tt.json), &resp)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, resp.Return.Status)
			assert.Equal(t, tt.expectedRunning, resp.Return.Running)
		})
	}
}
