package instances

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// getInstanceLogs returns the last N lines of instance console logs
func (m *manager) getInstanceLogs(
	ctx context.Context,
	id string,
	follow bool,
	tail int,
) (string, error) {
	// 1. Load instance
	meta, err := m.loadMetadata(id)
	if err != nil {
		return "", err
	}

	inst := meta.ToInstance()
	logPath := filepath.Join(inst.DataDir, "logs", "console.log")

	// 2. Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		return "", nil // No logs yet
	}

	// 3. For now, only support tail (not follow)
	if follow {
		return "", fmt.Errorf("follow not yet implemented")
	}

	// 4. Read last N lines
	return tailFile(logPath, tail)
}

// tailFile reads the last n lines from a file efficiently
func tailFile(path string, n int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open log file: %w", err)
	}
	defer file.Close()

	// For simplicity, read entire file and take last N lines
	// TODO: Optimize for very large log files with reverse reading
	var lines []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read log file: %w", err)
	}

	// Take last n lines
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}

	result := ""
	for _, line := range lines[start:] {
		result += line + "\n"
	}

	return result, nil
}

// followLogFile streams log file contents (for SSE implementation)
// Returns a channel that emits new log lines
func followLogFile(ctx context.Context, path string) (<-chan string, error) {
	// TODO: Implement with fsnotify or tail -f equivalent
	return nil, fmt.Errorf("not implemented")
}

