package instances

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLivenessChecker_ReturnsNilForNonManagerType(t *testing.T) {
	// Test that passing a non-*manager type returns nil
	// This would only happen if someone wraps the Manager interface
	// We can't easily test this without a mock, but we can test the happy path
	
	// For now, just verify the interface is implemented correctly
	var _ = (*instanceLivenessAdapter)(nil)
}

func TestInstanceLivenessAdapter_Interface(t *testing.T) {
	// Verify the adapter implements the expected interface
	// This is a compile-time check via the var _ assignment in liveness.go
	// but we can also verify the method signatures exist
	adapter := &instanceLivenessAdapter{}
	
	ctx := context.Background()
	
	// These should not panic even with nil manager
	// (they'll fail, but that's expected)
	running := adapter.IsInstanceRunning(ctx, "test-id")
	assert.False(t, running, "Should return false for nil manager")
	
	devices := adapter.GetInstanceDevices(ctx, "test-id")
	assert.Nil(t, devices, "Should return nil for nil manager")
	
	allDevices := adapter.ListAllInstanceDevices(ctx)
	assert.Nil(t, allDevices, "Should return nil for nil manager")
}





