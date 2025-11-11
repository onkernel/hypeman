package instances

import (
	"context"
	"path/filepath"
)

// listInstances returns all instances
func (m *manager) listInstances(ctx context.Context) ([]Instance, error) {
	files, err := m.listMetadataFiles()
	if err != nil {
		return nil, err
	}

	result := make([]Instance, 0, len(files))
	for _, file := range files {
		// Extract instance ID from path
		// Path format: {dataDir}/guests/{id}/metadata.json
		id := filepath.Base(filepath.Dir(file))

		meta, err := m.loadMetadata(id)
		if err != nil {
			// Skip instances with invalid metadata
			continue
		}

		result = append(result, meta.Instance)
	}

	return result, nil
}

// getInstance returns a single instance by ID
func (m *manager) getInstance(ctx context.Context, id string) (*Instance, error) {
	meta, err := m.loadMetadata(id)
	if err != nil {
		return nil, err
	}

	return meta.ToInstance(), nil
}

