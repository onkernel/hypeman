package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Config holds the parsed configuration from the config disk.
type Config struct {
	// Container execution parameters
	Entrypoint string
	Cmd        string
	Workdir    string

	// Environment variables
	Env map[string]string

	// Network configuration
	NetworkEnabled bool
	GuestIP        string
	GuestCIDR      string
	GuestGW        string
	GuestDNS       string

	// GPU passthrough
	HasGPU bool

	// Volume mounts (format: "device:path:mode[:overlay_device] ...")
	VolumeMounts []VolumeMount

	// Init mode: "exec" (default) or "systemd"
	InitMode string
}

// VolumeMount represents a volume mount configuration.
type VolumeMount struct {
	Device        string
	Path          string
	Mode          string // "ro", "rw", or "overlay"
	OverlayDevice string // Only used for overlay mode
}

// readConfig mounts and reads the config disk, parsing the shell configuration.
func readConfig(log *Logger) (*Config, error) {
	const configMount = "/mnt/config"
	const configFile = "/mnt/config/config.sh"

	// Create mount point
	if err := os.MkdirAll(configMount, 0755); err != nil {
		return nil, fmt.Errorf("mkdir config mount: %w", err)
	}

	// Mount config disk (/dev/vdc) read-only
	cmd := exec.Command("/bin/mount", "-o", "ro", "/dev/vdc", configMount)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mount config disk: %s: %s", err, output)
	}
	log.Info("config", "mounted config disk")

	// Read and parse config.sh
	cfg, err := parseConfigFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	log.Info("config", "parsed configuration")

	return cfg, nil
}

// parseConfigFile parses a shell-style configuration file.
// It handles simple KEY=VALUE and KEY="VALUE" assignments.
func parseConfigFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &Config{
		Env:      make(map[string]string),
		InitMode: "exec", // Default to exec mode
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle export statements
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimPrefix(line, "export ")
		}

		// Parse KEY=VALUE
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := unquote(strings.TrimSpace(parts[1]))

		switch key {
		case "ENTRYPOINT":
			cfg.Entrypoint = value
		case "CMD":
			cfg.Cmd = value
		case "WORKDIR":
			cfg.Workdir = value
		case "GUEST_IP":
			cfg.GuestIP = value
			cfg.NetworkEnabled = true
		case "GUEST_CIDR":
			cfg.GuestCIDR = value
		case "GUEST_GW":
			cfg.GuestGW = value
		case "GUEST_DNS":
			cfg.GuestDNS = value
		case "HAS_GPU":
			cfg.HasGPU = value == "1"
		case "VOLUME_MOUNTS":
			cfg.VolumeMounts = parseVolumeMounts(value)
		case "INIT_MODE":
			cfg.InitMode = value
		default:
			// Treat as environment variable
			cfg.Env[key] = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// parseVolumeMounts parses the VOLUME_MOUNTS string.
// Format: "device:path:mode[:overlay_device] device:path:mode ..."
func parseVolumeMounts(s string) []VolumeMount {
	if s == "" {
		return nil
	}

	var mounts []VolumeMount
	for _, vol := range strings.Fields(s) {
		parts := strings.Split(vol, ":")
		if len(parts) < 3 {
			continue
		}

		mount := VolumeMount{
			Device: parts[0],
			Path:   parts[1],
			Mode:   parts[2],
		}

		if len(parts) >= 4 {
			mount.OverlayDevice = parts[3]
		}

		mounts = append(mounts, mount)
	}

	return mounts
}

// unquote removes surrounding quotes from a string.
// Handles both single and double quotes.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}

	// Handle double quotes
	if s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}

	// Handle single quotes
	if s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}

	return s
}

