package ingress

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/onkernel/hypeman/lib/logger"
	"github.com/onkernel/hypeman/lib/paths"
)

// EnvoyDaemon manages the Envoy proxy daemon lifecycle.
// Envoy uses file-based xDS for dynamic configuration - it watches LDS/CDS files
// and automatically reloads when they change. No hot restart needed.
type EnvoyDaemon struct {
	paths          *paths.Paths
	adminAddress   string
	adminPort      int
	pid            int
	stopOnShutdown bool // If true, stop Envoy when hypeman shuts down
}

// NewEnvoyDaemon creates a new EnvoyDaemon manager.
func NewEnvoyDaemon(p *paths.Paths, adminAddress string, adminPort int, stopOnShutdown bool) *EnvoyDaemon {
	return &EnvoyDaemon{
		paths:          p,
		adminAddress:   adminAddress,
		adminPort:      adminPort,
		stopOnShutdown: stopOnShutdown,
	}
}

// StopOnShutdown returns whether Envoy should be stopped when hypeman shuts down.
func (d *EnvoyDaemon) StopOnShutdown() bool {
	return d.stopOnShutdown
}

// Start starts the Envoy daemon. If Envoy is already running (discovered via PID file
// or admin API), this is a no-op and returns the existing PID.
func (d *EnvoyDaemon) Start(ctx context.Context) (int, error) {
	// Check if already running
	if pid, running := d.DiscoverRunning(); running {
		d.pid = pid
		return pid, nil
	}

	return d.startEnvoy(ctx)
}

// startEnvoy starts a new Envoy process.
func (d *EnvoyDaemon) startEnvoy(ctx context.Context) (int, error) {
	// Get binary path (extracts if needed)
	binaryPath, err := GetEnvoyBinaryPath(d.paths)
	if err != nil {
		return 0, fmt.Errorf("get envoy binary: %w", err)
	}

	// Ensure envoy directory exists
	envoyDir := d.paths.EnvoyDir()
	if err := os.MkdirAll(envoyDir, 0755); err != nil {
		return 0, fmt.Errorf("create envoy directory: %w", err)
	}

	// Build command arguments - Envoy uses file-based xDS for dynamic config
	configPath := d.paths.EnvoyConfig()
	args := []string{
		"--config-path", configPath,
		"--log-path", d.paths.EnvoyLogFile(),
		"--log-level", "info",
	}

	// Use Command (not CommandContext) so process survives parent context cancellation
	cmd := exec.Command(binaryPath, args...)

	// Daemonize: create new session to fully detach from parent
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Create new session (implies new process group)
	}

	// Redirect stdout/stderr to log file
	logFile, err := os.OpenFile(
		filepath.Join(envoyDir, "envoy-stdout.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		return 0, fmt.Errorf("create stdout log: %w", err)
	}
	defer logFile.Close()

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start envoy: %w", err)
	}

	pid := cmd.Process.Pid

	// Write PID file
	pidPath := d.paths.EnvoyPIDFile()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		// Non-fatal, log but continue
		log := logger.FromContext(ctx)
		log.WarnContext(ctx, "failed to write PID file", "error", err)
	}

	// Wait for admin API to be ready
	waitCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := d.waitForAdmin(waitCtx); err != nil {
		// Try to kill the process if it failed to start properly
		if proc, err := os.FindProcess(pid); err == nil {
			proc.Kill()
		}
		return 0, fmt.Errorf("envoy failed to start: %w", err)
	}

	d.pid = pid
	return pid, nil
}

// Stop gracefully stops the Envoy daemon.
func (d *EnvoyDaemon) Stop() error {
	pid, running := d.DiscoverRunning()
	if !running {
		return nil // Already stopped
	}

	// Try graceful shutdown via admin API first
	client := &http.Client{Timeout: 5 * time.Second}
	adminURL := fmt.Sprintf("http://%s:%d/quitquitquit", d.adminAddress, d.adminPort)
	resp, err := client.Post(adminURL, "", nil)
	if err == nil {
		resp.Body.Close()
		// Wait for process to exit
		time.Sleep(2 * time.Second)
	}

	// Check if still running, send SIGTERM
	if d.isProcessRunning(pid) {
		proc, err := os.FindProcess(pid)
		if err == nil {
			proc.Signal(syscall.SIGTERM)
			time.Sleep(2 * time.Second)
		}
	}

	// Final check, send SIGKILL if needed
	if d.isProcessRunning(pid) {
		proc, err := os.FindProcess(pid)
		if err == nil {
			proc.Signal(syscall.SIGKILL)
		}
	}

	// Clean up PID file
	os.Remove(d.paths.EnvoyPIDFile())
	d.pid = 0

	return nil
}

// ReloadConfig is a no-op - Envoy watches the xDS config files and reloads automatically.
// This method is kept for API compatibility but does nothing since file-based xDS
// handles configuration updates automatically when files change.
func (d *EnvoyDaemon) ReloadConfig() error {
	// No-op: Envoy watches the LDS/CDS files and reloads automatically
	return nil
}

// DiscoverRunning checks if Envoy is already running and returns its PID.
// Returns (pid, true) if running, (0, false) otherwise.
func (d *EnvoyDaemon) DiscoverRunning() (int, bool) {
	// First, try to read PID file
	pidPath := d.paths.EnvoyPIDFile()
	data, err := os.ReadFile(pidPath)
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && d.isProcessRunning(pid) {
			// Verify it's actually Envoy by checking admin API
			if d.isAdminResponding() {
				return pid, true
			}
		}
	}

	// Try admin API directly (might be running without PID file)
	if d.isAdminResponding() {
		// We don't know the PID, but it's running
		// Try to find it via procfs
		pid := d.findEnvoyPID()
		if pid > 0 {
			// Update PID file
			os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0644)
			return pid, true
		}
	}

	return 0, false
}

// IsRunning returns true if Envoy is currently running.
func (d *EnvoyDaemon) IsRunning() bool {
	_, running := d.DiscoverRunning()
	return running
}

// GetPID returns the PID of the running Envoy process, or 0 if not running.
func (d *EnvoyDaemon) GetPID() int {
	pid, running := d.DiscoverRunning()
	if running {
		return pid
	}
	return 0
}

// AdminURL returns the admin API URL.
func (d *EnvoyDaemon) AdminURL() string {
	return fmt.Sprintf("http://%s:%d", d.adminAddress, d.adminPort)
}

// ValidateConfig validates an Envoy configuration file without starting Envoy.
// Returns nil if valid, or an error with details if invalid.
func (d *EnvoyDaemon) ValidateConfig(configPath string) error {
	binaryPath, err := GetEnvoyBinaryPath(d.paths)
	if err != nil {
		return fmt.Errorf("get envoy binary: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath,
		"--mode", "validate",
		"--config-path", configPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("config validation failed: %w: %s", err, string(output))
	}

	return nil
}

// waitForAdmin waits for the admin API to become responsive.
func (d *EnvoyDaemon) waitForAdmin(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for envoy admin API")
		case <-ticker.C:
			if d.isAdminResponding() {
				return nil
			}
		}
	}
}

// isAdminResponding checks if the admin API is responding.
func (d *EnvoyDaemon) isAdminResponding() bool {
	client := &http.Client{Timeout: 1 * time.Second}
	adminURL := fmt.Sprintf("http://%s:%d/ready", d.adminAddress, d.adminPort)
	resp, err := client.Get(adminURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// isProcessRunning checks if a process with the given PID is running.
func (d *EnvoyDaemon) isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Use signal 0 to check if process exists.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// findEnvoyPID tries to find the Envoy process PID by scanning /proc.
func (d *EnvoyDaemon) findEnvoyPID() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}

	configPath := d.paths.EnvoyConfig()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Read cmdline
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}

		// Check if it's envoy with our config path
		cmdStr := string(cmdline)
		if strings.Contains(cmdStr, "envoy") && strings.Contains(cmdStr, configPath) {
			return pid
		}
	}

	return 0
}
