package instances

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/onkernel/hypeman/lib/images"
)

// createConfigDisk generates an erofs disk with instance configuration
// The disk contains:
// - /config.sh - Shell script sourced by init
// - /metadata.json - JSON metadata for programmatic access
func (m *manager) createConfigDisk(inst *Instance, imageInfo *images.Image) error {
	// Create temporary directory for config files
	tmpDir, err := os.MkdirTemp("", "hypeman-config-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate config.sh
	configScript := m.generateConfigScript(inst, imageInfo)
	configPath := filepath.Join(tmpDir, "config.sh")
	if err := os.WriteFile(configPath, []byte(configScript), 0644); err != nil {
		return fmt.Errorf("write config.sh: %w", err)
	}

	// Generate metadata.json
	configMeta := map[string]interface{}{
		"instance_id":   inst.Id,
		"instance_name": inst.Name,
		"image":         inst.Image,
		"entrypoint":    imageInfo.Entrypoint,
		"cmd":           imageInfo.Cmd,
		"workdir":       imageInfo.WorkingDir,
		"env":           mergeEnv(imageInfo.Env, inst.Env),
	}
	metaData, err := json.MarshalIndent(configMeta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	metaPath := filepath.Join(tmpDir, "metadata.json")
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return fmt.Errorf("write metadata.json: %w", err)
	}

	// Create erofs disk with lz4 compression
	diskPath := filepath.Join(m.dataDir, "guests", inst.Id, "config.erofs")
	cmd := exec.Command("mkfs.erofs", "-zlz4", diskPath, tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkfs.erofs failed: %w: %s", err, output)
	}

	return nil
}

// generateConfigScript creates the shell script that will be sourced by init
func (m *manager) generateConfigScript(inst *Instance, imageInfo *images.Image) string {
	var script strings.Builder

	script.WriteString("#!/bin/sh\n")
	script.WriteString("# Generated config for instance: " + inst.Id + "\n\n")

	// Set entrypoint
	if len(imageInfo.Entrypoint) > 0 {
		script.WriteString("# Entrypoint from image\n")
		script.WriteString(fmt.Sprintf("ENTRYPOINT=%s\n", shellQuoteArray(imageInfo.Entrypoint)))
	} else {
		script.WriteString("ENTRYPOINT=\"\"\n")
	}

	// Set CMD
	if len(imageInfo.Cmd) > 0 {
		script.WriteString("# CMD from image\n")
		script.WriteString(fmt.Sprintf("CMD=%s\n", shellQuoteArray(imageInfo.Cmd)))
	} else {
		script.WriteString("CMD=\"\"\n")
	}

	// Set working directory
	if imageInfo.WorkingDir != "" {
		script.WriteString(fmt.Sprintf("WORKDIR=%s\n", shellQuote(imageInfo.WorkingDir)))
	} else {
		script.WriteString("WORKDIR=\"/\"\n")
	}

	script.WriteString("\n# Environment variables\n")

	// Merge image env and instance env
	mergedEnv := mergeEnv(imageInfo.Env, inst.Env)

	// Export all environment variables
	for key, value := range mergedEnv {
		script.WriteString(fmt.Sprintf("export %s=%s\n", key, shellQuote(value)))
	}

	return script.String()
}

// mergeEnv merges image environment variables with instance overrides
func mergeEnv(imageEnv map[string]string, instEnv map[string]string) map[string]string {
	result := make(map[string]string)

	// Start with image env
	for k, v := range imageEnv {
		result[k] = v
	}

	// Override with instance env
	for k, v := range instEnv {
		result[k] = v
	}

	return result
}

// shellQuote quotes a string for safe use in shell scripts
func shellQuote(s string) string {
	// Simple quoting: wrap in single quotes and escape single quotes
	s = strings.ReplaceAll(s, "'", "'\\''")
	return "'" + s + "'"
}

// shellQuoteArray quotes an array of strings as a space-separated shell string
func shellQuoteArray(arr []string) string {
	if len(arr) == 0 {
		return "\"\""
	}

	quoted := make([]string, len(arr))
	for i, s := range arr {
		quoted[i] = shellQuote(s)
	}

	return strings.Join(quoted, " ")
}


