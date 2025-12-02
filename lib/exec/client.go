package exec

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// vsockDialTimeout is the timeout for connecting to the vsock Unix socket
	vsockDialTimeout = 5 * time.Second
	// vsockHandshakeTimeout is the timeout for the Cloud Hypervisor vsock handshake
	vsockHandshakeTimeout = 5 * time.Second
	// vsockGuestPort is the port the exec-agent listens on inside the guest
	vsockGuestPort = 2222
)

// ExitStatus represents command exit information
type ExitStatus struct {
	Code int
}

// ExecOptions configures command execution
type ExecOptions struct {
	Command []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	TTY     bool
	Env     map[string]string // Environment variables
	Cwd     string            // Working directory (optional)
	Timeout int32             // Execution timeout in seconds (0 = no timeout)
}

// bufferedConn wraps a net.Conn with a bufio.Reader to ensure any buffered
// data from the handshake is properly drained before reading from the connection
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

// ExecIntoInstance executes command in instance via vsock using gRPC
// vsockSocketPath is the Unix socket created by Cloud Hypervisor (e.g., /var/lib/hypeman/guests/{id}/vsock.sock)
func ExecIntoInstance(ctx context.Context, vsockSocketPath string, opts ExecOptions) (*ExitStatus, error) {
	// Connect to Cloud Hypervisor's vsock Unix socket with custom dialer
	grpcConn, err := grpc.NewClient("passthrough:///vsock",
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dialVsock(ctx, vsockSocketPath)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("create grpc client: %w", err)
	}
	defer grpcConn.Close()

	// Create exec client
	client := NewExecServiceClient(grpcConn)
	stream, err := client.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("start exec stream: %w", err)
	}

	// Send start request
	if err := stream.Send(&ExecRequest{
		Request: &ExecRequest_Start{
			Start: &ExecStart{
				Command:        opts.Command,
				Tty:            opts.TTY,
				Env:            opts.Env,
				Cwd:            opts.Cwd,
				TimeoutSeconds: opts.Timeout,
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("send start request: %w", err)
	}

	// Handle stdin in background
	if opts.Stdin != nil {
		go func() {
			buf := make([]byte, 32 * 1024)
			for {
				n, err := opts.Stdin.Read(buf)
				if n > 0 {
					stream.Send(&ExecRequest{
						Request: &ExecRequest_Stdin{Stdin: buf[:n]},
					})
				}
				if err != nil {
					stream.CloseSend()
					return
				}
			}
		}()
	}

	// Receive responses
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			// Stream closed without exit code
			return nil, fmt.Errorf("stream closed without exit code")
		}
		if err != nil {
			return nil, fmt.Errorf("receive response: %w", err)
		}

		switch r := resp.Response.(type) {
		case *ExecResponse_Stdout:
			if opts.Stdout != nil {
				opts.Stdout.Write(r.Stdout)
			}
		case *ExecResponse_Stderr:
			if opts.Stderr != nil {
				opts.Stderr.Write(r.Stderr)
			}
		case *ExecResponse_ExitCode:
			return &ExitStatus{Code: int(r.ExitCode)}, nil
		}
	}
}

// dialVsock connects to Cloud Hypervisor's vsock Unix socket and performs the handshake
func dialVsock(ctx context.Context, vsockSocketPath string) (net.Conn, error) {
	slog.Debug("connecting to vsock", "socket", vsockSocketPath)

	// Use dial timeout, respecting context deadline if shorter
	dialTimeout := vsockDialTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < dialTimeout {
			dialTimeout = remaining
		}
	}

	// Connect to CH's Unix socket with timeout
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", vsockSocketPath)
	if err != nil {
		return nil, fmt.Errorf("dial vsock socket %s: %w", vsockSocketPath, err)
	}

	slog.Debug("connected to vsock socket, performing handshake", "port", vsockGuestPort)

	// Set deadline for handshake
	if err := conn.SetDeadline(time.Now().Add(vsockHandshakeTimeout)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set handshake deadline: %w", err)
	}

	// Perform Cloud Hypervisor vsock handshake
	handshakeCmd := fmt.Sprintf("CONNECT %d\n", vsockGuestPort)
	if _, err := conn.Write([]byte(handshakeCmd)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send vsock handshake: %w", err)
	}

	// Read handshake response
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read vsock handshake response (is exec-agent running in guest?): %w", err)
	}

	// Clear deadline after successful handshake
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("clear deadline: %w", err)
	}

	response = strings.TrimSpace(response)
	if !strings.HasPrefix(response, "OK ") {
		conn.Close()
		return nil, fmt.Errorf("vsock handshake failed: %s", response)
	}

	slog.Debug("vsock handshake successful", "response", response)

	// Return wrapped connection that uses the bufio.Reader
	// This ensures any bytes buffered during handshake are not lost
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

