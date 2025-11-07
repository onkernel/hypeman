package images

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/onkernel/hypeman/lib/oapi"
)

const (
	StatusPending    = "pending"
	StatusPulling    = "pulling"
	StatusUnpacking  = "unpacking"
	StatusConverting = "converting"
	StatusReady      = "ready"
	StatusFailed     = "failed"
)

// Manager handles image lifecycle operations
type Manager interface {
	ListImages(ctx context.Context) ([]oapi.Image, error)
	CreateImage(ctx context.Context, req oapi.CreateImageRequest) (*oapi.Image, error)
	GetImage(ctx context.Context, id string) (*oapi.Image, error)
	DeleteImage(ctx context.Context, id string) error
	RecoverInterruptedBuilds()
}

type manager struct {
	dataDir   string
	ociClient *OCIClient
	queue     *BuildQueue
}

// NewManager creates a new image manager with OCI client
func NewManager(dataDir string, ociClient *OCIClient, maxConcurrentBuilds int) Manager {
	m := &manager{
		dataDir:   dataDir,
		ociClient: ociClient,
		queue:     NewBuildQueue(maxConcurrentBuilds),
	}
	m.RecoverInterruptedBuilds()
	return m
}

func (m *manager) ListImages(ctx context.Context) ([]oapi.Image, error) {
	metas, err := listMetadata(m.dataDir)
	if err != nil {
		return nil, fmt.Errorf("list metadata: %w", err)
	}

	images := make([]oapi.Image, 0, len(metas))
	for _, meta := range metas {
		images = append(images, *meta.toOAPI())
	}

	return images, nil
}

func (m *manager) CreateImage(ctx context.Context, req oapi.CreateImageRequest) (*oapi.Image, error) {
	imageID := req.Id
	if imageID == nil || *imageID == "" {
		generated := generateImageID(req.Name)
		imageID = &generated
	}

	if imageExists(m.dataDir, *imageID) {
		return nil, ErrAlreadyExists
	}

	meta := &imageMetadata{
		ID:        *imageID,
		Name:      req.Name,
		Status:    StatusPending,
		Request:   &req,
		CreatedAt: time.Now(),
	}

	if err := writeMetadata(m.dataDir, *imageID, meta); err != nil {
		return nil, fmt.Errorf("write initial metadata: %w", err)
	}

	queuePos := m.queue.Enqueue(*imageID, req, func() {
		m.buildImage(context.Background(), *imageID, req)
	})

	img := meta.toOAPI()
	if queuePos > 0 {
		img.QueuePosition = &queuePos
	}
	return img, nil
}

func (m *manager) buildImage(ctx context.Context, imageID string, req oapi.CreateImageRequest) {
	defer m.queue.MarkComplete(imageID)

	buildDir := filepath.Join(imageDir(m.dataDir, imageID), ".build")
	tempDir := filepath.Join(buildDir, "rootfs")

	if err := os.MkdirAll(buildDir, 0755); err != nil {
		m.updateStatus(imageID, StatusFailed, fmt.Errorf("create build dir: %w", err))
		return
	}

	defer func() {
		meta, _ := readMetadata(m.dataDir, imageID)
		if meta != nil && meta.Status == StatusReady {
			os.RemoveAll(buildDir)
		}
	}()

	m.updateStatus(imageID, StatusPulling, nil)
	containerMeta, err := m.ociClient.pullAndExport(ctx, req.Name, tempDir)
	if err != nil {
		m.updateStatus(imageID, StatusFailed, fmt.Errorf("pull and export: %w", err))
		return
	}

	m.updateStatus(imageID, StatusUnpacking, nil)
	
	m.updateStatus(imageID, StatusConverting, nil)
	diskPath := imagePath(m.dataDir, imageID)
	diskSize, err := convertToExt4(tempDir, diskPath)
	if err != nil {
		m.updateStatus(imageID, StatusFailed, fmt.Errorf("convert to ext4: %w", err))
		return
	}

	meta, err := readMetadata(m.dataDir, imageID)
	if err != nil {
		m.updateStatus(imageID, StatusFailed, fmt.Errorf("read metadata: %w", err))
		return
	}

	meta.Status = StatusReady
	meta.Error = nil
	meta.SizeBytes = diskSize
	meta.Entrypoint = containerMeta.Entrypoint
	meta.Cmd = containerMeta.Cmd
	meta.Env = containerMeta.Env
	meta.WorkingDir = containerMeta.WorkingDir

	if err := writeMetadata(m.dataDir, imageID, meta); err != nil {
		m.updateStatus(imageID, StatusFailed, fmt.Errorf("write final metadata: %w", err))
		return
	}
}

func (m *manager) updateStatus(imageID, status string, err error) {
	meta, readErr := readMetadata(m.dataDir, imageID)
	if readErr != nil {
		return
	}

	meta.Status = status
	if err != nil {
		errorMsg := err.Error()
		meta.Error = &errorMsg
	}

	writeMetadata(m.dataDir, imageID, meta)
}

func (m *manager) RecoverInterruptedBuilds() {
	metas, err := listMetadata(m.dataDir)
	if err != nil {
		return // Best effort
	}

	// Sort by created_at to maintain FIFO order
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].CreatedAt.Before(metas[j].CreatedAt)
	})

	for _, meta := range metas {
		switch meta.Status {
		case StatusPending, StatusPulling, StatusUnpacking, StatusConverting:
			if meta.Request != nil {
				metaCopy := meta
				m.queue.Enqueue(metaCopy.ID, *metaCopy.Request, func() {
					m.buildImage(context.Background(), metaCopy.ID, *metaCopy.Request)
				})
			}
		}
	}
}

func (m *manager) GetImage(ctx context.Context, id string) (*oapi.Image, error) {
	meta, err := readMetadata(m.dataDir, id)
	if err != nil {
		return nil, err
	}
	
	img := meta.toOAPI()
	
	// Inject live queue position if pending
	if meta.Status == StatusPending {
		img.QueuePosition = m.queue.GetPosition(id)
	}
	
	return img, nil
}

func (m *manager) DeleteImage(ctx context.Context, id string) error {
	return deleteImage(m.dataDir, id)
}

// generateImageID creates a valid ID from an image name
// Example: docker.io/library/nginx:latest -> img-nginx-latest
func generateImageID(imageName string) string {
	// Extract image name and tag
	parts := strings.Split(imageName, "/")
	nameTag := parts[len(parts)-1]

	// Replace special characters with dashes
	reg := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	sanitized := reg.ReplaceAllString(nameTag, "-")
	sanitized = strings.Trim(sanitized, "-")

	// Add prefix
	return "img-" + sanitized
}


