package instances

import (
	"context"

	"github.com/onkernel/hypeman/lib/devices"
)

// Ensure instanceLivenessAdapter implements the interface
var _ devices.InstanceLivenessChecker = (*instanceLivenessAdapter)(nil)

// instanceLivenessAdapter adapts instances.Manager to devices.InstanceLivenessChecker
type instanceLivenessAdapter struct {
	manager *manager
}

// NewLivenessChecker creates a new InstanceLivenessChecker that wraps the instances manager.
// This adapter allows the devices package to query instance state without a circular import.
func NewLivenessChecker(m Manager) devices.InstanceLivenessChecker {
	// Type assert to get the concrete manager type
	mgr, ok := m.(*manager)
	if !ok {
		return nil
	}
	return &instanceLivenessAdapter{manager: mgr}
}

// IsInstanceRunning returns true if the instance exists and is in a running state
// (i.e., has an active VMM process). Returns false if the instance doesn't exist
// or is stopped/standby/unknown.
func (a *instanceLivenessAdapter) IsInstanceRunning(ctx context.Context, instanceID string) bool {
	if a.manager == nil {
		return false
	}
	inst, err := a.manager.getInstance(ctx, instanceID)
	if err != nil {
		return false
	}

	// Consider instance "running" if the VMM is active (any of these states means VM is using the device)
	switch inst.State {
	case StateRunning, StatePaused, StateCreated:
		return true
	default:
		// StateStopped, StateStandby, StateShutdown, StateUnknown
		return false
	}
}

// GetInstanceDevices returns the list of device IDs attached to an instance.
// Returns nil if the instance doesn't exist.
func (a *instanceLivenessAdapter) GetInstanceDevices(ctx context.Context, instanceID string) []string {
	if a.manager == nil {
		return nil
	}
	inst, err := a.manager.getInstance(ctx, instanceID)
	if err != nil {
		return nil
	}
	return inst.Devices
}

// ListAllInstanceDevices returns a map of instanceID -> []deviceIDs for all instances.
func (a *instanceLivenessAdapter) ListAllInstanceDevices(ctx context.Context) map[string][]string {
	if a.manager == nil {
		return nil
	}
	instances, err := a.manager.listInstances(ctx)
	if err != nil {
		return nil
	}

	result := make(map[string][]string)
	for _, inst := range instances {
		if len(inst.Devices) > 0 {
			result[inst.Id] = inst.Devices
		}
	}
	return result
}

