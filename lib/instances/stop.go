package instances

import (
	"context"
	"fmt"
	"time"

	"github.com/onkernel/hypeman/lib/exec"
	"github.com/onkernel/hypeman/lib/logger"
	"github.com/onkernel/hypeman/lib/network"
	"github.com/onkernel/hypeman/lib/vmm"
	"go.opentelemetry.io/otel/trace"
)

// stopInstance gracefully stops a running instance
// Multi-hop orchestration: Running → Shutdown → Stopped
func (m *manager) stopInstance(
	ctx context.Context,
	id string,
) (*Instance, error) {
	start := time.Now()
	log := logger.FromContext(ctx)
	log.InfoContext(ctx, "stopping instance", "id", id)

	// Start tracing span if tracer is available
	if m.metrics != nil && m.metrics.tracer != nil {
		var span trace.Span
		ctx, span = m.metrics.tracer.Start(ctx, "StopInstance")
		defer span.End()
	}

	// 1. Load instance
	meta, err := m.loadMetadata(id)
	if err != nil {
		log.ErrorContext(ctx, "failed to load instance metadata", "id", id, "error", err)
		return nil, err
	}

	inst := m.toInstance(ctx, meta)
	stored := &meta.StoredMetadata
	log.DebugContext(ctx, "loaded instance", "id", id, "state", inst.State)

	// 2. Validate state transition (must be Running to stop)
	if inst.State != StateRunning {
		log.ErrorContext(ctx, "invalid state for stop", "id", id, "state", inst.State)
		return nil, fmt.Errorf("%w: cannot stop from state %s, must be Running", ErrInvalidState, inst.State)
	}

	// 3. Get network allocation BEFORE killing VMM (while we can still query it)
	var networkAlloc *network.Allocation
	if inst.NetworkEnabled {
		log.DebugContext(ctx, "getting network allocation", "id", id)
		networkAlloc, err = m.networkManager.GetAllocation(ctx, id)
		if err != nil {
			log.WarnContext(ctx, "failed to get network allocation, will still attempt cleanup", "id", id, "error", err)
		}
	}

	// 4. Send graceful shutdown signal via vsock
	// This signals init (PID 1) to shut down, which forwards SIGTERM to the app
	log.DebugContext(ctx, "sending shutdown signal via vsock", "id", id)
	_, execErr := exec.ExecIntoInstance(ctx, inst.VsockSocket, exec.ExecOptions{
		Command: []string{"kill", "-TERM", "1"},
		Timeout: 2, // 2 second timeout for the kill command
	})
	if execErr != nil {
		// Log but continue - exec-agent might not be available, fall back to VMM shutdown
		log.WarnContext(ctx, "failed to send shutdown via vsock, will force stop", "id", id, "error", execErr)
	} else {
		log.DebugContext(ctx, "shutdown signal sent via vsock", "id", id)
	}

	// 5. Wait for VM to shut down gracefully
	// Poll VMM state to detect when the guest has shut down
	gracefulTimeout := 2 * time.Second
	pollInterval := 50 * time.Millisecond
	deadline := time.Now().Add(gracefulTimeout)

	client, err := vmm.NewVMM(inst.SocketPath)
	if err != nil {
		log.WarnContext(ctx, "failed to create VMM client for polling", "id", id, "error", err)
	}

	gracefulShutdown := false
	for client != nil && time.Now().Before(deadline) {
		infoResp, err := client.GetVmInfoWithResponse(ctx)
		if err != nil {
			// VMM not responding - guest has shut down
			gracefulShutdown = true
			log.DebugContext(ctx, "VMM not responding, guest has shut down", "id", id)
			break
		}
		if infoResp.StatusCode() == 200 && infoResp.JSON200 != nil {
			if infoResp.JSON200.State == vmm.Shutdown {
				gracefulShutdown = true
				log.DebugContext(ctx, "VM shut down gracefully", "id", id)
				break
			}
		}
		time.Sleep(pollInterval)
	}

	if !gracefulShutdown {
		log.DebugContext(ctx, "graceful shutdown timeout, stopping VMM directly", "id", id)
	}

	// 6. Transition: Shutdown → Stopped (shutdown VMM process)
	log.DebugContext(ctx, "shutting down VMM", "id", id)
	if err := m.shutdownVMM(ctx, &inst); err != nil {
		// Log but continue - try to clean up anyway
		log.WarnContext(ctx, "failed to shutdown VMM gracefully", "id", id, "error", err)
	}

	// 7. Release network allocation (delete TAP device)
	if inst.NetworkEnabled && networkAlloc != nil {
		log.DebugContext(ctx, "releasing network", "id", id, "network", "default")
		if err := m.networkManager.ReleaseAllocation(ctx, networkAlloc); err != nil {
			// Log error but continue
			log.WarnContext(ctx, "failed to release network, continuing", "id", id, "error", err)
		}
	}

	// 8. Update metadata (clear PID, set StoppedAt)
	now := time.Now()
	stored.StoppedAt = &now
	stored.CHPID = nil

	meta = &metadata{StoredMetadata: *stored}
	if err := m.saveMetadata(meta); err != nil {
		log.ErrorContext(ctx, "failed to save metadata", "id", id, "error", err)
		return nil, fmt.Errorf("save metadata: %w", err)
	}

	// Record metrics
	if m.metrics != nil {
		m.recordDuration(ctx, m.metrics.stopDuration, start, "success")
		m.recordStateTransition(ctx, string(StateRunning), string(StateStopped))
	}

	// Return instance with derived state (should be Stopped now)
	finalInst := m.toInstance(ctx, meta)
	log.InfoContext(ctx, "instance stopped successfully", "id", id, "state", finalInst.State)
	return &finalInst, nil
}
