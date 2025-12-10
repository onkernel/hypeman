// Package logger provides structured logging with subsystem-specific levels
// and OpenTelemetry trace context integration.
package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// InstanceLogHandler wraps an slog.Handler and additionally writes logs
// that have an "id" attribute to a per-instance hypeman.log file.
// This provides automatic per-instance logging without manual instrumentation.
//
// Implementation follows the slog handler guide for shared state across
// WithAttrs/WithGroup: https://pkg.go.dev/golang.org/x/example/slog-handler-guide
type InstanceLogHandler struct {
	slog.Handler
	logPathFunc func(id string) string // returns path to hypeman.log for an instance
	state       *sharedState           // shared across all handlers derived via WithAttrs/WithGroup
}

// sharedState holds state that must be shared across all handler instances
// derived from the same parent via WithAttrs/WithGroup.
// Using a pointer ensures all derived handlers share the same mutex and file cache.
type sharedState struct {
	mu        sync.Mutex
	fileCache map[string]*os.File
}

// NewInstanceLogHandler creates a new handler that wraps the given handler
// and writes instance-related logs to per-instance log files.
// logPathFunc should return the path to hypeman.log for a given instance ID.
func NewInstanceLogHandler(wrapped slog.Handler, logPathFunc func(id string) string) *InstanceLogHandler {
	return &InstanceLogHandler{
		Handler:     wrapped,
		logPathFunc: logPathFunc,
		state: &sharedState{
			fileCache: make(map[string]*os.File),
		},
	}
}

// Handle processes a log record, passing it to the wrapped handler and
// optionally writing to a per-instance log file if "id" attribute is present.
func (h *InstanceLogHandler) Handle(ctx context.Context, r slog.Record) error {
	// Always pass to wrapped handler first
	if err := h.Handler.Handle(ctx, r); err != nil {
		return err
	}

	// Check for instance ID in attributes
	var instanceID string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "id" {
			instanceID = a.Value.String()
			return false // stop iteration
		}
		return true
	})

	// If instance ID found, also write to per-instance log
	if instanceID != "" {
		h.writeToInstanceLog(instanceID, r)
	}

	return nil
}

// writeToInstanceLog writes a log record to the instance's hypeman.log file.
func (h *InstanceLogHandler) writeToInstanceLog(instanceID string, r slog.Record) {
	logPath := h.logPathFunc(instanceID)
	if logPath == "" {
		return
	}

	// Format log line outside the lock: timestamp LEVEL message key=value key=value...
	timestamp := r.Time.Format(time.RFC3339)
	level := r.Level.String()
	msg := r.Message

	// Collect attributes (excluding "id" since it's implicit)
	var attrs []string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "id" {
			attrs = append(attrs, fmt.Sprintf("%s=%v", a.Key, a.Value))
		}
		return true
	})

	// Build log line
	line := fmt.Sprintf("%s %s %s", timestamp, level, msg)
	for _, attr := range attrs {
		line += " " + attr
	}
	line += "\n"

	// Get or create file handle and write (single lock acquisition)
	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	f, ok := h.state.fileCache[instanceID]
	if !ok {
		// Ensure directory exists
		dir := filepath.Dir(logPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return // silently skip if can't create directory
		}

		var err error
		f, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return // silently skip if can't open file
		}
		h.state.fileCache[instanceID] = f
	}

	// Write to file (best effort)
	f.WriteString(line)
}

// Enabled reports whether the handler handles records at the given level.
func (h *InstanceLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

// WithAttrs returns a new handler with the given attributes.
// The new handler shares the same state (mutex and file cache) as the parent.
func (h *InstanceLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &InstanceLogHandler{
		Handler:     h.Handler.WithAttrs(attrs),
		logPathFunc: h.logPathFunc,
		state:       h.state, // same pointer = shared mutex and cache
	}
}

// WithGroup returns a new handler with the given group name.
// The new handler shares the same state (mutex and file cache) as the parent.
func (h *InstanceLogHandler) WithGroup(name string) slog.Handler {
	return &InstanceLogHandler{
		Handler:     h.Handler.WithGroup(name),
		logPathFunc: h.logPathFunc,
		state:       h.state, // same pointer = shared mutex and cache
	}
}

// CloseInstanceLog closes and removes a cached file handle for an instance.
// Call this when an instance is deleted.
func (h *InstanceLogHandler) CloseInstanceLog(instanceID string) {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	if f, ok := h.state.fileCache[instanceID]; ok {
		f.Close()
		delete(h.state.fileCache, instanceID)
	}
}

// CloseAll closes all cached file handles.
// Call this during shutdown.
func (h *InstanceLogHandler) CloseAll() {
	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	for id, f := range h.state.fileCache {
		f.Close()
		delete(h.state.fileCache, id)
	}
}
