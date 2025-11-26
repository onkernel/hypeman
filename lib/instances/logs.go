package instances

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/onkernel/hypeman/lib/logger"
)

// getInstanceLogs returns the last N lines of instance console logs
func (m *manager) getInstanceLogs(
	ctx context.Context,
	id string,
	tail int,
) (string, error) {
	log := logger.FromContext(ctx)
	log.DebugContext(ctx, "getting instance logs", "id", id, "tail", tail)

	// 1. Verify instance exists
	_, err := m.loadMetadata(id)
	if err != nil {
		log.ErrorContext(ctx, "failed to load instance metadata", "id", id, "error", err)
		return "", err
	}

	logPath := m.paths.InstanceConsoleLog(id)

	// 2. Check if log file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		log.DebugContext(ctx, "no log file exists yet", "id", id)
		return "", nil // No logs yet
	}

	// 3. Read last N lines
	result, err := tailFile(logPath, tail)
	if err != nil {
		log.ErrorContext(ctx, "failed to read log file", "id", id, "error", err)
		return "", err
	}

	log.DebugContext(ctx, "retrieved instance logs", "id", id, "bytes", len(result))
	return result, nil
}

// streamInstanceLogs streams instance console logs
// First sends the last N lines, then polls for new content
func (m *manager) streamInstanceLogs(
	ctx context.Context,
	id string,
	tail int,
) (<-chan string, error) {
	log := logger.FromContext(ctx)
	log.DebugContext(ctx, "starting log stream", "id", id, "tail", tail)

	// 1. Verify instance exists
	_, err := m.loadMetadata(id)
	if err != nil {
		log.ErrorContext(ctx, "failed to load instance metadata", "id", id, "error", err)
		return nil, err
	}

	logPath := m.paths.InstanceConsoleLog(id)

	// 2. Create output channel
	out := make(chan string, 100)

	go func() {
		defer close(out)

		// 3. Send initial tail lines
		initialLines, offset, err := tailFileWithOffset(logPath, tail)
		if err != nil {
			log.ErrorContext(ctx, "failed to read initial logs", "id", id, "error", err)
			return
		}

		for _, line := range initialLines {
			select {
			case <-ctx.Done():
				return
			case out <- line:
			}
		}

		// 4. Poll for new content
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.DebugContext(ctx, "log stream cancelled", "id", id)
				return
			case <-ticker.C:
				newLines, newOffset, err := readNewLines(logPath, offset)
				if err != nil {
					log.ErrorContext(ctx, "failed to read new log lines", "id", id, "error", err)
					continue
				}
				offset = newOffset

				for _, line := range newLines {
					select {
					case <-ctx.Done():
						return
					case out <- line:
					}
				}
			}
		}
	}()

	return out, nil
}

// tailFile reads the last n lines from a file
func tailFile(path string, n int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
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

// tailFileWithOffset reads the last n lines and returns them with the file offset
func tailFileWithOffset(path string, n int) ([]string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("open log file: %w", err)
	}
	defer file.Close()

	// Read all lines
	var lines []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("read log file: %w", err)
	}

	// Get current file position (end of file)
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, 0, fmt.Errorf("seek log file: %w", err)
	}

	// Take last n lines
	start := 0
	if len(lines) > n {
		start = len(lines) - n
	}

	return lines[start:], offset, nil
}

// readNewLines reads new lines from a file starting at the given offset
func readNewLines(path string, offset int64) ([]string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, offset, nil
		}
		return nil, offset, fmt.Errorf("open log file: %w", err)
	}
	defer file.Close()

	// Get current file size
	info, err := file.Stat()
	if err != nil {
		return nil, offset, fmt.Errorf("stat log file: %w", err)
	}

	// If file was truncated (offset > size), reset to beginning
	if offset > info.Size() {
		offset = 0
	}

	// No new content
	if info.Size() == offset {
		return nil, offset, nil
	}

	// Seek to offset
	_, err = file.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, offset, fmt.Errorf("seek log file: %w", err)
	}

	// Read new lines
	var lines []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, offset, fmt.Errorf("read log file: %w", err)
	}

	// Get new offset
	newOffset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, offset, fmt.Errorf("get position: %w", err)
	}

	return lines, newOffset, nil
}
