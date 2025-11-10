package images

import "time"

// Image represents a container image converted to bootable disk
type Image struct {
	Name          string
	Status        string
	QueuePosition *int
	Error         *string
	SizeBytes     *int64
	Entrypoint    []string
	Cmd           []string
	Env           map[string]string
	WorkingDir    string
	CreatedAt     time.Time
}

// CreateImageRequest represents a request to create an image
type CreateImageRequest struct {
	Name string
}

