package images

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/onkernel/hypeman/lib/oapi"
)

type imageMetadata struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Status     string                   `json:"status"`
	Error      *string                  `json:"error,omitempty"`
	Request    *oapi.CreateImageRequest `json:"request,omitempty"`
	SizeBytes  int64                    `json:"size_bytes"`
	Entrypoint []string                 `json:"entrypoint,omitempty"`
	Cmd        []string                 `json:"cmd,omitempty"`
	Env        map[string]string        `json:"env,omitempty"`
	WorkingDir string                   `json:"working_dir,omitempty"`
	CreatedAt  time.Time                `json:"created_at"`
}

func (m *imageMetadata) toOAPI() *oapi.Image {
	img := &oapi.Image{
		Id:        m.ID,
		Name:      m.Name,
		Status:    oapi.ImageStatus(m.Status),
		Error:     m.Error,
		CreatedAt: m.CreatedAt,
	}

	if m.Status == StatusReady && m.SizeBytes > 0 {
		sizeBytes := m.SizeBytes
		img.SizeBytes = &sizeBytes
	}

	if len(m.Entrypoint) > 0 {
		img.Entrypoint = &m.Entrypoint
	}
	if len(m.Cmd) > 0 {
		img.Cmd = &m.Cmd
	}
	if len(m.Env) > 0 {
		img.Env = &m.Env
	}
	if m.WorkingDir != "" {
		img.WorkingDir = &m.WorkingDir
	}

	return img
}

func imageDir(dataDir, imageID string) string {
	return filepath.Join(dataDir, "images", imageID)
}

func imagePath(dataDir, imageID string) string {
	return filepath.Join(imageDir(dataDir, imageID), "rootfs.ext4")
}

func metadataPath(dataDir, imageID string) string {
	return filepath.Join(imageDir(dataDir, imageID), "metadata.json")
}

func writeMetadata(dataDir, imageID string, meta *imageMetadata) error {
	dir := imageDir(dataDir, imageID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create image directory: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	tempPath := metadataPath(dataDir, imageID) + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("write temp metadata: %w", err)
	}

	finalPath := metadataPath(dataDir, imageID)
	if err := os.Rename(tempPath, finalPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename metadata: %w", err)
	}

	return nil
}

func readMetadata(dataDir, imageID string) (*imageMetadata, error) {
	path := metadataPath(dataDir, imageID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var meta imageMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}

	if meta.Status == StatusReady {
		diskPath := imagePath(dataDir, imageID)
		if _, err := os.Stat(diskPath); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("disk image missing: %s", diskPath)
			}
			return nil, fmt.Errorf("stat disk image: %w", err)
		}
	}

	return &meta, nil
}

func listMetadata(dataDir string) ([]*imageMetadata, error) {
	imagesDir := filepath.Join(dataDir, "images")
	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*imageMetadata{}, nil
		}
		return nil, fmt.Errorf("read images directory: %w", err)
	}

	var metas []*imageMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		meta, err := readMetadata(dataDir, entry.Name())
		if err != nil {
			continue
		}
		metas = append(metas, meta)
	}

	return metas, nil
}

func imageExists(dataDir, imageID string) bool {
	_, err := readMetadata(dataDir, imageID)
	return err == nil
}

func deleteImage(dataDir, imageID string) error {
	dir := imageDir(dataDir, imageID)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("stat image directory: %w", err)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove image directory: %w", err)
	}

	return nil
}
