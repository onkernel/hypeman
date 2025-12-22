package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/mdlayher/vsock"
	pb "github.com/onkernel/hypeman/lib/guest"
	"google.golang.org/grpc"
)

// guestServer implements the gRPC GuestService
type guestServer struct {
	pb.UnimplementedGuestServiceServer
}

func main() {
	// Listen on vsock port 2222 with retries
	var l *vsock.Listener
	var err error
	
	for i := 0; i < 10; i++ {
		l, err = vsock.Listen(2222, nil)
		if err == nil {
			break
		}
		log.Printf("[guest-agent] vsock listen attempt %d/10 failed: %v (retrying in 1s)", i+1, err)
		time.Sleep(1 * time.Second)
	}
	
	if err != nil {
		log.Fatalf("[guest-agent] failed to listen on vsock port 2222 after retries: %v", err)
	}
	defer l.Close()

	log.Println("[guest-agent] listening on vsock port 2222")

	// Create gRPC server
	grpcServer := grpc.NewServer()
	pb.RegisterGuestServiceServer(grpcServer, &guestServer{})

	// Serve gRPC over vsock
	if err := grpcServer.Serve(l); err != nil {
		log.Fatalf("[guest-agent] gRPC server failed: %v", err)
	}
}

// Exec handles command execution with bidirectional streaming
func (s *guestServer) Exec(stream pb.GuestService_ExecServer) error {
	log.Printf("[guest-agent] new exec stream")

	// Receive start request
	req, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("receive start request: %w", err)
	}

	start := req.GetStart()
	if start == nil {
		return fmt.Errorf("first message must be ExecStart")
	}

	command := start.Command
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}

	log.Printf("[guest-agent] exec: command=%v tty=%v cwd=%s timeout=%d",
		command, start.Tty, start.Cwd, start.TimeoutSeconds)

	// Create context with timeout if specified
	ctx := context.Background()
	if start.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(start.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	if start.Tty {
		return s.executeTTY(ctx, stream, start)
	}
	return s.executeNoTTY(ctx, stream, start)
}

// executeNoTTY executes command without TTY
func (s *guestServer) executeNoTTY(ctx context.Context, stream pb.GuestService_ExecServer, start *pb.ExecStart) error {
	// Run command directly - guest-agent is already running in container namespace
	if len(start.Command) == 0 {
		return fmt.Errorf("empty command")
	}
	
	cmd := exec.CommandContext(ctx, start.Command[0], start.Command[1:]...)
	
	// Set up environment
	cmd.Env = s.buildEnv(start.Env)
	
	// Set up working directory
	if start.Cwd != "" {
		cmd.Dir = start.Cwd
	}

	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	// Mutex to protect concurrent stream.Send calls (gRPC streams are not thread-safe)
	var sendMu sync.Mutex

	// Use WaitGroup to ensure all output is read before sending
	var wg sync.WaitGroup
	var stdoutData, stderrData []byte

	// Handle stdin in background
	go func() {
		defer stdin.Close()
		for {
			req, err := stream.Recv()
			if err != nil {
				return
			}
			if data := req.GetStdin(); data != nil {
				stdin.Write(data)
			}
		}
	}()

	// Read all stdout/stderr BEFORE calling Wait() - Wait() closes the pipes!
	wg.Add(1)
	go func() {
		defer wg.Done()
		data, _ := io.ReadAll(stdout)
		stdoutData = data
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		data, _ := io.ReadAll(stderr)
		stderrData = data
	}()

	// Wait for all reads to complete FIRST (before Wait closes pipes)
	wg.Wait()
	
	// Now safe to call Wait - pipes are fully drained
	waitErr := cmd.Wait()

	// Now stream output in chunks (streaming compatible)
	const chunkSize = 32 * 1024
	for i := 0; i < len(stdoutData); i += chunkSize {
		end := i + chunkSize
		if end > len(stdoutData) {
			end = len(stdoutData)
		}
		sendMu.Lock()
		stream.Send(&pb.ExecResponse{
			Response: &pb.ExecResponse_Stdout{Stdout: stdoutData[i:end]},
		})
		sendMu.Unlock()
	}
	for i := 0; i < len(stderrData); i += chunkSize {
		end := i + chunkSize
		if end > len(stderrData) {
			end = len(stderrData)
		}
		sendMu.Lock()
		stream.Send(&pb.ExecResponse{
			Response: &pb.ExecResponse_Stderr{Stderr: stderrData[i:end]},
		})
		sendMu.Unlock()
	}

	exitCode := int32(0)
	if cmd.ProcessState != nil {
		exitCode = int32(cmd.ProcessState.ExitCode())
	} else if waitErr != nil {
		// If killed by timeout, exit with 124 (GNU timeout convention)
		exitCode = 124
	}

	log.Printf("[guest-agent] command finished with exit code: %d", exitCode)

	// Send exit code
	return stream.Send(&pb.ExecResponse{
		Response: &pb.ExecResponse_ExitCode{ExitCode: exitCode},
	})
}

// executeTTY executes command with TTY
func (s *guestServer) executeTTY(ctx context.Context, stream pb.GuestService_ExecServer, start *pb.ExecStart) error {
	// Run command directly with PTY - guest-agent is already running in container namespace
	// This ensures PTY and shell are in the same namespace, fixing Ctrl+C signal handling
	if len(start.Command) == 0 {
		return fmt.Errorf("empty command")
	}
	
	cmd := exec.CommandContext(ctx, start.Command[0], start.Command[1:]...)
	
	// Set up environment
	cmd.Env = s.buildEnv(start.Env)
	
	// Set up working directory
	if start.Cwd != "" {
		cmd.Dir = start.Cwd
	}
	
	// Start with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}
	defer ptmx.Close()

	// Mutex to protect concurrent stream.Send calls (gRPC streams are not thread-safe)
	var sendMu sync.Mutex

	// Use WaitGroup to ensure all output is sent before exit code
	var wg sync.WaitGroup

	// Handle stdin in background
	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				return
			}

			if data := req.GetStdin(); data != nil {
				ptmx.Write(data)
			}
		}
	}()

	// Stream output
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32 * 1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				sendMu.Lock()
				stream.Send(&pb.ExecResponse{
					Response: &pb.ExecResponse_Stdout{Stdout: buf[:n]},
				})
				sendMu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	// Wait for command or context cancellation
	waitErr := cmd.Wait()
	
	// Wait for all output to be sent
	wg.Wait()

	exitCode := int32(0)
	if cmd.ProcessState != nil {
		exitCode = int32(cmd.ProcessState.ExitCode())
	} else if waitErr != nil {
		// If killed by timeout, exit with 124 (GNU timeout convention)
		exitCode = 124
	}

	log.Printf("[guest-agent] TTY command finished with exit code: %d", exitCode)

	// Send exit code
	return stream.Send(&pb.ExecResponse{
		Response: &pb.ExecResponse_ExitCode{ExitCode: exitCode},
	})
}

// buildEnv constructs environment variables by merging provided env with defaults
func (s *guestServer) buildEnv(envMap map[string]string) []string {
	// Start with current environment as base
	env := os.Environ()
	
	// Merge in provided environment variables
	for k, v := range envMap {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	
	return env
}

// CopyToGuest handles copying files to the guest filesystem
func (s *guestServer) CopyToGuest(stream pb.GuestService_CopyToGuestServer) error {
	log.Printf("[guest-agent] new copy-to-guest stream")

	// Receive start request
	req, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("receive start request: %w", err)
	}

	start := req.GetStart()
	if start == nil {
		return stream.SendAndClose(&pb.CopyToGuestResponse{
			Success: false,
			Error:   "first message must be CopyToGuestStart",
		})
	}

	log.Printf("[guest-agent] copy-to-guest: path=%s mode=%o is_dir=%v size=%d",
		start.Path, start.Mode, start.IsDir, start.Size)

	// Handle directory creation
	if start.IsDir {
		// Check if destination exists and is a file
		if info, err := os.Stat(start.Path); err == nil && !info.IsDir() {
			return stream.SendAndClose(&pb.CopyToGuestResponse{
				Success: false,
				Error:   fmt.Sprintf("cannot create directory: %s is a file", start.Path),
			})
		}

		if err := os.MkdirAll(start.Path, fs.FileMode(start.Mode)); err != nil {
			return stream.SendAndClose(&pb.CopyToGuestResponse{
				Success: false,
				Error:   fmt.Sprintf("create directory: %v", err),
			})
		}
		// Wait for end message
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return stream.SendAndClose(&pb.CopyToGuestResponse{
					Success: false,
					Error:   fmt.Sprintf("receive: %v", err),
				})
			}
			if req.GetEnd() != nil {
				break
			}
		}
		return stream.SendAndClose(&pb.CopyToGuestResponse{
			Success:      true,
			BytesWritten: 0,
		})
	}

	// Create parent directories if needed
	dir := filepath.Dir(start.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return stream.SendAndClose(&pb.CopyToGuestResponse{
			Success: false,
			Error:   fmt.Sprintf("create parent directory: %v", err),
		})
	}

	// Check if destination exists and is a directory
	if info, err := os.Stat(start.Path); err == nil && info.IsDir() {
		return stream.SendAndClose(&pb.CopyToGuestResponse{
			Success: false,
			Error:   fmt.Sprintf("cannot copy file: %s is a directory", start.Path),
		})
	}

	// Create file
	file, err := os.OpenFile(start.Path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(start.Mode))
	if err != nil {
		return stream.SendAndClose(&pb.CopyToGuestResponse{
			Success: false,
			Error:   fmt.Sprintf("create file: %v", err),
		})
	}
	defer file.Close()

	var bytesWritten int64

	// Receive data chunks
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return stream.SendAndClose(&pb.CopyToGuestResponse{
				Success: false,
				Error:   fmt.Sprintf("receive: %v", err),
			})
		}

		if data := req.GetData(); data != nil {
			n, err := file.Write(data)
			if err != nil {
				return stream.SendAndClose(&pb.CopyToGuestResponse{
					Success: false,
					Error:   fmt.Sprintf("write: %v", err),
				})
			}
			bytesWritten += int64(n)
		}

		if req.GetEnd() != nil {
			break
		}
	}

	// Set modification time if provided
	if start.Mtime > 0 {
		mtime := time.Unix(start.Mtime, 0)
		os.Chtimes(start.Path, mtime, mtime)
	}

	// Set ownership if provided (archive mode)
	// Only chown when both UID and GID are explicitly set (non-zero)
	// to avoid accidentally setting one to root (0) when only the other is specified
	if start.Uid > 0 && start.Gid > 0 {
		if err := os.Chown(start.Path, int(start.Uid), int(start.Gid)); err != nil {
			log.Printf("[guest-agent] warning: failed to set ownership on %s: %v", start.Path, err)
		}
	}

	log.Printf("[guest-agent] copy-to-guest complete: %d bytes written to %s", bytesWritten, start.Path)

	return stream.SendAndClose(&pb.CopyToGuestResponse{
		Success:      true,
		BytesWritten: bytesWritten,
	})
}

// CopyFromGuest handles copying files from the guest filesystem
func (s *guestServer) CopyFromGuest(req *pb.CopyFromGuestRequest, stream pb.GuestService_CopyFromGuestServer) error {
	log.Printf("[guest-agent] copy-from-guest: path=%s follow_links=%v", req.Path, req.FollowLinks)

	// Stat the source path
	var info os.FileInfo
	var err error
	if req.FollowLinks {
		info, err = os.Stat(req.Path)
	} else {
		info, err = os.Lstat(req.Path)
	}
	if err != nil {
		return stream.Send(&pb.CopyFromGuestResponse{
			Response: &pb.CopyFromGuestResponse_Error{
				Error: &pb.CopyFromGuestError{
					Message: fmt.Sprintf("stat: %v", err),
					Path:    req.Path,
				},
			},
		})
	}

	if info.IsDir() {
		// Walk directory and stream all files
		return s.copyFromGuestDir(req.Path, req.FollowLinks, stream)
	}

	// Single file
	return s.copyFromGuestFile(req.Path, "", info, req.FollowLinks, stream, true)
}

// copyFromGuestFile streams a single file
func (s *guestServer) copyFromGuestFile(fullPath, relativePath string, info os.FileInfo, followLinks bool, stream pb.GuestService_CopyFromGuestServer, isFinal bool) error {
	if relativePath == "" {
		relativePath = filepath.Base(fullPath)
	}

	// Check if it's a symlink
	isSymlink := info.Mode()&os.ModeSymlink != 0
	var linkTarget string
	if isSymlink && !followLinks {
		target, err := os.Readlink(fullPath)
		if err != nil {
			return stream.Send(&pb.CopyFromGuestResponse{
				Response: &pb.CopyFromGuestResponse_Error{
					Error: &pb.CopyFromGuestError{
						Message: fmt.Sprintf("readlink: %v", err),
						Path:    fullPath,
					},
				},
			})
		}
		linkTarget = target
	}

	// Extract UID/GID from file info
	var uid, gid uint32
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		uid = stat.Uid
		gid = stat.Gid
	}

	// Send header
	header := &pb.CopyFromGuestHeader{
		Path:       relativePath,
		Mode:       uint32(info.Mode().Perm()),
		IsDir:      false,
		IsSymlink:  isSymlink && !followLinks,
		LinkTarget: linkTarget,
		Size:       info.Size(),
		Mtime:      info.ModTime().Unix(),
		Uid:        uid,
		Gid:        gid,
	}

	if err := stream.Send(&pb.CopyFromGuestResponse{
		Response: &pb.CopyFromGuestResponse_Header{Header: header},
	}); err != nil {
		return err
	}

	// If it's a symlink and we're not following, we're done with this file
	if isSymlink && !followLinks {
		return stream.Send(&pb.CopyFromGuestResponse{
			Response: &pb.CopyFromGuestResponse_End{End: &pb.CopyFromGuestEnd{Final: isFinal}},
		})
	}

	// Stream file content
	file, err := os.Open(fullPath)
	if err != nil {
		return stream.Send(&pb.CopyFromGuestResponse{
			Response: &pb.CopyFromGuestResponse_Error{
				Error: &pb.CopyFromGuestError{
					Message: fmt.Sprintf("open: %v", err),
					Path:    fullPath,
				},
			},
		})
	}
	defer file.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&pb.CopyFromGuestResponse{
				Response: &pb.CopyFromGuestResponse_Data{Data: buf[:n]},
			}); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return stream.Send(&pb.CopyFromGuestResponse{
				Response: &pb.CopyFromGuestResponse_Error{
					Error: &pb.CopyFromGuestError{
						Message: fmt.Sprintf("read: %v", err),
						Path:    fullPath,
					},
				},
			})
		}
	}

	// Send end marker
	return stream.Send(&pb.CopyFromGuestResponse{
		Response: &pb.CopyFromGuestResponse_End{End: &pb.CopyFromGuestEnd{Final: isFinal}},
	})
}

// StatPath returns information about a path in the guest filesystem
func (s *guestServer) StatPath(ctx context.Context, req *pb.StatPathRequest) (*pb.StatPathResponse, error) {
	log.Printf("[guest-agent] stat-path: path=%s follow_links=%v", req.Path, req.FollowLinks)

	var info os.FileInfo
	var err error
	if req.FollowLinks {
		info, err = os.Stat(req.Path)
	} else {
		info, err = os.Lstat(req.Path)
	}

	if err != nil {
		if os.IsNotExist(err) {
			return &pb.StatPathResponse{
				Exists: false,
			}, nil
		}
		return &pb.StatPathResponse{
			Exists: false,
			Error:  err.Error(),
		}, nil
	}

	resp := &pb.StatPathResponse{
		Exists: true,
		IsDir:  info.IsDir(),
		IsFile: info.Mode().IsRegular(),
		Mode:   uint32(info.Mode().Perm()),
		Size:   info.Size(),
	}

	// Check if it's a symlink (only relevant if follow_links=false)
	if info.Mode()&os.ModeSymlink != 0 {
		resp.IsSymlink = true
		target, err := os.Readlink(req.Path)
		if err == nil {
			resp.LinkTarget = target
		}
	}

	return resp, nil
}

// copyFromGuestDir walks a directory and streams all files
func (s *guestServer) copyFromGuestDir(rootPath string, followLinks bool, stream pb.GuestService_CopyFromGuestServer) error {
	// Collect all entries first to know which is final
	type entry struct {
		fullPath     string
		relativePath string
		info         os.FileInfo
	}
	var entries []entry

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Send error but continue
			stream.Send(&pb.CopyFromGuestResponse{
				Response: &pb.CopyFromGuestResponse_Error{
					Error: &pb.CopyFromGuestError{
						Message: fmt.Sprintf("walk: %v", err),
						Path:    path,
					},
				},
			})
			return nil
		}

		// Use os.Stat when followLinks is true to get the target's info
		// Use d.Info() (same as os.Lstat) when followLinks is false to get symlink's info
		var info os.FileInfo
		if followLinks {
			info, err = os.Stat(path)
		} else {
			info, err = d.Info()
		}
		if err != nil {
			stream.Send(&pb.CopyFromGuestResponse{
				Response: &pb.CopyFromGuestResponse_Error{
					Error: &pb.CopyFromGuestError{
						Message: fmt.Sprintf("info: %v", err),
						Path:    path,
					},
				},
			})
			return nil
		}

		relPath, _ := filepath.Rel(rootPath, path)
		if relPath == "." {
			relPath = filepath.Base(rootPath)
		} else {
			relPath = filepath.Join(filepath.Base(rootPath), relPath)
		}

		entries = append(entries, entry{
			fullPath:     path,
			relativePath: relPath,
			info:         info,
		})
		return nil
	})
	if err != nil {
		return stream.Send(&pb.CopyFromGuestResponse{
			Response: &pb.CopyFromGuestResponse_Error{
				Error: &pb.CopyFromGuestError{
					Message: fmt.Sprintf("walk directory: %v", err),
					Path:    rootPath,
				},
			},
		})
	}

	// Stream each entry
	for i, e := range entries {
		isFinal := i == len(entries)-1

		if e.info.IsDir() {
			// Extract UID/GID from file info
			var uid, gid uint32
			if stat, ok := e.info.Sys().(*syscall.Stat_t); ok {
				uid = stat.Uid
				gid = stat.Gid
			}

			// Send directory header
			if err := stream.Send(&pb.CopyFromGuestResponse{
				Response: &pb.CopyFromGuestResponse_Header{
					Header: &pb.CopyFromGuestHeader{
						Path:  e.relativePath,
						Mode:  uint32(e.info.Mode().Perm()),
						IsDir: true,
						Mtime: e.info.ModTime().Unix(),
						Uid:   uid,
						Gid:   gid,
					},
				},
			}); err != nil {
				return err
			}
			// Send end for directory
			if err := stream.Send(&pb.CopyFromGuestResponse{
				Response: &pb.CopyFromGuestResponse_End{End: &pb.CopyFromGuestEnd{Final: isFinal}},
			}); err != nil {
				return err
			}
		} else {
			if err := s.copyFromGuestFile(e.fullPath, e.relativePath, e.info, followLinks, stream, isFinal); err != nil {
				return err
			}
		}
	}

	log.Printf("[guest-agent] copy-from-guest complete: %d entries from %s", len(entries), rootPath)
	return nil
}

