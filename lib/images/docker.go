package images

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// DockerClient wraps Docker API operations
type DockerClient struct {
	cli *client.Client
}

// NewDockerClient creates a new Docker client using environment variables
func NewDockerClient() (*DockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	return &DockerClient{cli: cli}, nil
}

// Close closes the Docker client
func (d *DockerClient) Close() error {
	return d.cli.Close()
}

// pullAndExport pulls an OCI image and exports its rootfs to a directory
func (d *DockerClient) pullAndExport(ctx context.Context, imageRef, exportDir string) (*containerMetadata, error) {
	// Pull the image
	pullReader, err := d.cli.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return nil, fmt.Errorf("pull image: %w", err)
	}
	// Consume the pull output (required for pull to complete)
	io.Copy(io.Discard, pullReader)
	pullReader.Close()

	// Inspect image to get metadata
	inspect, _, err := d.cli.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("inspect image: %w", err)
	}

	// Extract metadata from inspect response
	meta := &containerMetadata{
		Entrypoint: inspect.Config.Entrypoint,
		Cmd:        inspect.Config.Cmd,
		Env:        make(map[string]string),
		WorkingDir: inspect.Config.WorkingDir,
	}

	// Parse environment variables
	for _, env := range inspect.Config.Env {
		for i := 0; i < len(env); i++ {
			if env[i] == '=' {
				key := env[:i]
				val := env[i+1:]
				meta.Env[key] = val
				break
			}
		}
	}

	// Create a temporary container from the image
	// Creating a container does not run it,
	// it does not execute any code inside the container.
	containerResp, err := d.cli.ContainerCreate(ctx, &container.Config{
		Image: imageRef,
	}, nil, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	// Ensure container is removed even if export fails
	defer func() {
		d.cli.ContainerRemove(ctx, containerResp.ID, container.RemoveOptions{})
	}()

	// Export container filesystem as tar stream
	exportReader, err := d.cli.ContainerExport(ctx, containerResp.ID)
	if err != nil {
		return nil, fmt.Errorf("export container: %w", err)
	}
	defer exportReader.Close()

	// Extract tar to exportDir
	if err := extractTar(exportReader, exportDir); err != nil {
		return nil, fmt.Errorf("extract tar: %w", err)
	}

	return meta, nil
}

// containerMetadata holds extracted container metadata
type containerMetadata struct {
	Entrypoint []string
	Cmd        []string
	Env        map[string]string
	WorkingDir string
}

// extractTar extracts a tar stream to a target directory
// Ignores ownership/permission errors (matches POC behavior: docker export | tar -C dir -xf -)
func extractTar(reader io.Reader, targetDir string) error {
	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		target := filepath.Join(targetDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory, ignore chmod errors (best effort)
			os.MkdirAll(target, 0755)

		case tar.TypeReg:
			// Create parent directory
			os.MkdirAll(filepath.Dir(target), 0755)

			// Create file, use default permissions if header.Mode fails
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
			if err != nil {
				return fmt.Errorf("create file %s: %w", target, err)
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return fmt.Errorf("write file %s: %w", target, err)
			}
			outFile.Close()

		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(target), 0755)
			os.Remove(target) // Remove if exists
			os.Symlink(header.Linkname, target) // Ignore errors

		case tar.TypeLink:
			linkTarget := filepath.Join(targetDir, header.Linkname)
			os.Remove(target)
			os.Link(linkTarget, target) // Ignore errors
		}
	}

	return nil
}
