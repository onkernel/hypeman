package qemu

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/digitalocean/go-qemu/qmp"
)

// QMPClient wraps go-qemu's QMP monitor with convenience methods.
type QMPClient struct {
	monitor *qmp.SocketMonitor
}

// NewQMPClient creates a new QMP client connected to the given socket.
func NewQMPClient(socketPath string) (*QMPClient, error) {
	monitor, err := qmp.NewSocketMonitor("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("create socket monitor: %w", err)
	}

	if err := monitor.Connect(); err != nil {
		return nil, fmt.Errorf("connect to qmp: %w", err)
	}

	return &QMPClient{monitor: monitor}, nil
}

// Close disconnects from the QMP socket.
func (c *QMPClient) Close() error {
	return c.monitor.Disconnect()
}

// Run executes a raw QMP command and returns the response.
func (c *QMPClient) Run(command []byte) ([]byte, error) {
	return c.monitor.Run(command)
}

// qmpCommand represents a QMP command structure
type qmpCommand struct {
	Execute   string      `json:"execute"`
	Arguments interface{} `json:"arguments,omitempty"`
}

// qmpStatusResponse represents the response from query-status
type qmpStatusResponse struct {
	Return struct {
		Running bool   `json:"running"`
		Status  string `json:"status"`
	} `json:"return"`
}

// Stop pauses VM execution (QMP 'stop' command).
func (c *QMPClient) Stop() error {
	cmd := qmpCommand{Execute: "stop"}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal stop command: %w", err)
	}

	_, err = c.monitor.Run(cmdBytes)
	if err != nil {
		return fmt.Errorf("execute stop: %w", err)
	}

	return nil
}

// Continue resumes VM execution (QMP 'cont' command).
func (c *QMPClient) Continue() error {
	cmd := qmpCommand{Execute: "cont"}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal cont command: %w", err)
	}

	_, err = c.monitor.Run(cmdBytes)
	if err != nil {
		return fmt.Errorf("execute cont: %w", err)
	}

	return nil
}

// QueryStatus returns the current VM status.
func (c *QMPClient) QueryStatus() (string, bool, error) {
	cmd := qmpCommand{Execute: "query-status"}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return "", false, fmt.Errorf("marshal query-status: %w", err)
	}

	resp, err := c.monitor.Run(cmdBytes)
	if err != nil {
		return "", false, fmt.Errorf("execute query-status: %w", err)
	}

	var statusResp qmpStatusResponse
	if err := json.Unmarshal(resp, &statusResp); err != nil {
		return "", false, fmt.Errorf("unmarshal status response: %w", err)
	}

	return statusResp.Return.Status, statusResp.Return.Running, nil
}

// Quit shuts down QEMU (QMP 'quit' command).
func (c *QMPClient) Quit() error {
	cmd := qmpCommand{Execute: "quit"}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal quit command: %w", err)
	}

	// quit command doesn't return a response - QEMU exits
	_, _ = c.monitor.Run(cmdBytes)
	return nil
}

// SystemPowerdown sends ACPI power button event (graceful shutdown).
func (c *QMPClient) SystemPowerdown() error {
	cmd := qmpCommand{Execute: "system_powerdown"}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal system_powerdown: %w", err)
	}

	_, err = c.monitor.Run(cmdBytes)
	if err != nil {
		return fmt.Errorf("execute system_powerdown: %w", err)
	}

	return nil
}

// SystemReset resets the VM (hard reset).
func (c *QMPClient) SystemReset() error {
	cmd := qmpCommand{Execute: "system_reset"}
	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal system_reset: %w", err)
	}

	_, err = c.monitor.Run(cmdBytes)
	if err != nil {
		return fmt.Errorf("execute system_reset: %w", err)
	}

	return nil
}
