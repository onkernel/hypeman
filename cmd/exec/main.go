package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

func main() {
	// Parse flags
	interactive := flag.Bool("it", false, "Interactive mode with TTY")
	token := flag.String("token", "", "JWT token (or use HYPEMAN_TOKEN env var)")
	apiURL := flag.String("api-url", "http://localhost:8080", "API server URL")
	flag.Parse()

	// Get instance ID and optional command
	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [-it] [-token TOKEN] [-api-url URL] <instance-id> [command...]\n", os.Args[0])
		os.Exit(1)
	}

	instanceID := args[0]
	var command []string
	if len(args) > 1 {
		command = args[1:]
	}

	// Get JWT token
	jwtToken := *token
	if jwtToken == "" {
		jwtToken = os.Getenv("HYPEMAN_TOKEN")
	}
	if jwtToken == "" {
		fmt.Fprintf(os.Stderr, "Error: JWT token required (use --token or HYPEMAN_TOKEN env var)\n")
		os.Exit(1)
	}

	// Build WebSocket URL
	u, err := url.Parse(*apiURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Invalid API URL: %v\n", err)
		os.Exit(1)
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = fmt.Sprintf("/instances/%s/exec", instanceID)

	// Add query parameters
	q := u.Query()
	if len(command) > 0 {
		for _, c := range command {
			q.Add("command", c)
		}
	}
	q.Set("tty", fmt.Sprintf("%t", *interactive))
	u.RawQuery = q.Encode()

	// Connect to WebSocket
	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", jwtToken))

	ws, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to connect: %v\n", err)
		os.Exit(1)
	}
	defer ws.Close()

	// Handle interactive mode
	if *interactive {
		if err := runInteractive(ws); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := runNonInteractive(ws); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

func runInteractive(ws *websocket.Conn) error {
	// Put terminal in raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Handle Ctrl-C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Channel for errors
	errCh := make(chan error, 2)

	// Forward stdin to WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				if err != io.EOF {
					errCh <- fmt.Errorf("stdin read error: %w", err)
				}
				return
			}
			if n > 0 {
				if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					errCh <- fmt.Errorf("websocket write error: %w", err)
					return
				}
			}
		}
	}()

	// Forward WebSocket to stdout
	go func() {
		for {
			_, message, err := ws.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					errCh <- fmt.Errorf("websocket read error: %w", err)
				}
				return
			}
			if _, err := os.Stdout.Write(message); err != nil {
				errCh <- fmt.Errorf("stdout write error: %w", err)
				return
			}
		}
	}()

	// Wait for error, signal, or completion
	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		return nil
	}
}

func runNonInteractive(ws *websocket.Conn) error {
	// Channel for errors
	errCh := make(chan error, 2)
	doneCh := make(chan struct{})

	// Forward stdin to WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				if err != io.EOF {
					errCh <- fmt.Errorf("stdin read error: %w", err)
				}
				return
			}
			if n > 0 {
				if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					errCh <- fmt.Errorf("websocket write error: %w", err)
					return
				}
			}
		}
	}()

	// Forward WebSocket to stdout
	go func() {
		defer close(doneCh)
		for {
			_, message, err := ws.ReadMessage()
			if err != nil {
				// Connection closed is normal - exit gracefully
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) || 
				   err == io.EOF {
					return
				}
				errCh <- fmt.Errorf("websocket read error: %w", err)
				return
			}
			if _, err := os.Stdout.Write(message); err != nil {
				errCh <- fmt.Errorf("stdout write error: %w", err)
				return
			}
		}
	}()

	// Wait for completion or error
	select {
	case err := <-errCh:
		return err
	case <-doneCh:
		return nil
	}
}

