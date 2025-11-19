package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/mdlayher/vsock"
)

// Logging helpers with [exec-agent] prefix
func logInfo(format string, v ...interface{}) {
	log.Printf("[exec-agent] "+format, v...)
}

func logError(format string, v ...interface{}) {
	log.Printf("[exec-agent] ERROR: "+format, v...)
}

const (
	StreamStdin  byte = 0
	StreamStdout byte = 1
	StreamStderr byte = 2
	StreamError  byte = 3
	StreamResize byte = 4
)

type ExecRequest struct {
	Command []string `json:"command"`
	TTY     bool     `json:"tty"`
}

type ResizeMessage struct {
	Width  uint16 `json:"width"`
	Height uint16 `json:"height"`
}

type ExitMessage struct {
	Status struct {
		Code int `json:"code"`
	} `json:"status"`
}

func main() {
	// Listen on vsock port 2222 using socket API
	// Retry a few times as virtio-vsock device may take a moment to initialize
	var l net.Listener
	var err error
	
	for i := 0; i < 10; i++ {
		l, err = vsock.Listen(2222, nil)
		if err == nil {
			break
		}
		logInfo("vsock listen attempt %d/10 failed: %v (retrying in 1s)", i+1, err)
		time.Sleep(1 * time.Second)
	}
	
	if err != nil {
		logError("failed to listen on vsock port 2222 after retries: %v", err)
		os.Exit(1)
	}
	defer l.Close()

	logInfo("listening on vsock port 2222")

	for {
		conn, err := l.Accept()
		if err != nil {
			logError("accept error: %v", err)
			continue
		}

		logInfo("accepted connection from %s", conn.RemoteAddr())
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			logError("handleConnection panicked: %v", r)
		}
		conn.Close()
	}()
	
	logInfo("handling connection from %s", conn.RemoteAddr())

	// Read first frame (should be exec request on stdin stream)
	streamType, data, err := readFrame(conn)
	if err != nil {
		logError("read request: %v", err)
		return
	}

	if streamType != StreamStdin {
		sendError(conn, "first message must be stdin with exec request")
		return
	}

	var req ExecRequest
	if err := json.Unmarshal(data, &req); err != nil {
		sendError(conn, fmt.Sprintf("invalid request: %v", err))
		return
	}

	if len(req.Command) == 0 {
		req.Command = []string{"/bin/sh"}
	}

	logInfo("exec: command=%v tty=%v", req.Command, req.TTY)

	if req.TTY {
		executeTTY(conn, req.Command)
	} else {
		executeNoTTY(conn, req.Command)
	}
}

func executeTTY(conn net.Conn, command []string) {
	// Chroot into container before executing
	cmd := exec.Command("chroot", append([]string{"/overlay/newroot"}, command...)...)
	cmd.Env = os.Environ()

	// Start with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		sendError(conn, fmt.Sprintf("start pty: %v", err))
		return
	}
	defer ptmx.Close()

	done := make(chan struct{})

	// Handle input (stdin + resize)
	go func() {
		defer close(done)
		for {
			streamType, data, err := readFrame(conn)
			if err != nil {
				return
			}

			switch streamType {
			case StreamStdin:
				ptmx.Write(data)
			case StreamResize:
				var resize ResizeMessage
				if err := json.Unmarshal(data, &resize); err == nil {
					pty.Setsize(ptmx, &pty.Winsize{
						Rows: resize.Height,
						Cols: resize.Width,
					})
				}
			}
		}
	}()

	// Stream output
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				sendFrame(conn, StreamStdout, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	<-done
	cmd.Wait()

	// Send exit code
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	sendExit(conn, exitCode) // Ignore error in TTY mode

	// Graceful shutdown
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.CloseWrite()
	} else if unixConn, ok := conn.(*net.UnixConn); ok {
		unixConn.CloseWrite()
	}
	io.Copy(io.Discard, conn)
}

func executeNoTTY(conn net.Conn, command []string) {
	// Chroot into container before executing
	cmd := exec.Command("chroot", append([]string{"/overlay/newroot"}, command...)...)
	cmd.Env = os.Environ()

	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		sendError(conn, fmt.Sprintf("start: %v", err))
		return
	}

	// Handle stdin in background (don't block on it)
	go func() {
		defer stdin.Close()
		for {
			streamType, data, err := readFrame(conn)
			if err != nil {
				return
			}
			if streamType == StreamStdin {
				stdin.Write(data)
			}
		}
	}()

	// Use channels to wait for stdout/stderr to finish
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})

	// Stream stdout
	go func() {
		defer close(stdoutDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				sendFrame(conn, StreamStdout, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Stream stderr
	go func() {
		defer close(stderrDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				sendFrame(conn, StreamStderr, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Wait for command to finish (don't wait for stdin)
	err := cmd.Wait()
	
	logInfo("command finished: err=%v", err)

	// Wait for stdout/stderr goroutines to finish reading all data
	logInfo("waiting for stdout to close...")
	<-stdoutDone
	logInfo("waiting for stderr to close...")
	<-stderrDone
	logInfo("stdout/stderr streams closed")

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	
	logInfo("computed exit code: %d, sending...", exitCode)
	if err := sendExit(conn, exitCode); err != nil {
		logError("error sending exit: %v", err)
		return
	}
	logInfo("exit sent successfully")
	
	// Close the write side to signal we're done
	// This sends a FIN packet but keeps the connection open for reading
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.CloseWrite()
	} else if unixConn, ok := conn.(*net.UnixConn); ok {
		unixConn.CloseWrite()
	}
	
	// Wait for client to close the connection by reading until EOF
	// This ensures the client has received all data including the exit code
	// properly before we fully close the socket.
	io.Copy(io.Discard, conn)
	
	logInfo("connection closed by client")
}

func readFrame(conn net.Conn) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}

	streamType := header[0]
	length := binary.BigEndian.Uint32(header[1:5])

	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return 0, nil, err
	}

	return streamType, data, nil
}

func sendFrame(conn net.Conn, streamType byte, data []byte) error {
	header := make([]byte, 5)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[1:5], uint32(len(data)))

	if _, err := conn.Write(header); err != nil {
		return err
	}
	if _, err := conn.Write(data); err != nil {
		return err
	}
	return nil
}

func sendError(conn net.Conn, msg string) {
	sendFrame(conn, StreamError, []byte(msg))
}

func sendExit(conn net.Conn, code int) error {
	exit := ExitMessage{}
	exit.Status.Code = code
	data, err := json.Marshal(exit)
	if err != nil {
		return err
	}
	return sendFrame(conn, StreamError, data)
}

