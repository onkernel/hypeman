package images

import (
	"errors"
	"strings"
)

var (
	ErrNotFound      = errors.New("image not found")
	ErrAlreadyExists = errors.New("image already exists")
)

// IsInvalidNameError checks if an error is due to invalid image name
func IsInvalidNameError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "invalid image name") ||
		strings.Contains(err.Error(), "invalid reference format")
}
