package builds

import "errors"

var (
	// ErrNotFound is returned when a build is not found
	ErrNotFound = errors.New("build not found")

	// ErrAlreadyExists is returned when a build with the same ID already exists
	ErrAlreadyExists = errors.New("build already exists")

	// ErrInvalidRuntime is returned when an unsupported runtime is specified
	// Deprecated: Runtime validation is no longer performed. The generic builder
	// accepts any Dockerfile.
	ErrInvalidRuntime = errors.New("invalid runtime")

	// ErrDockerfileRequired is returned when no Dockerfile is provided
	ErrDockerfileRequired = errors.New("dockerfile required: provide dockerfile parameter or include Dockerfile in source tarball")

	// ErrBuildFailed is returned when a build fails
	ErrBuildFailed = errors.New("build failed")

	// ErrBuildTimeout is returned when a build exceeds its timeout
	ErrBuildTimeout = errors.New("build timeout")

	// ErrBuildCancelled is returned when a build is cancelled
	ErrBuildCancelled = errors.New("build cancelled")

	// ErrInvalidSource is returned when the source tarball is invalid
	ErrInvalidSource = errors.New("invalid source")

	// ErrSourceHashMismatch is returned when the source hash doesn't match
	ErrSourceHashMismatch = errors.New("source hash mismatch")

	// ErrBuilderNotReady is returned when the builder image is not available
	ErrBuilderNotReady = errors.New("builder image not ready")

	// ErrBuildInProgress is returned when trying to cancel a build that's already complete
	ErrBuildInProgress = errors.New("build in progress")
)

// IsSupportedRuntime returns true if the runtime is supported.
// Deprecated: This function always returns true. The generic builder system
// no longer validates runtimes - users provide their own Dockerfile.
func IsSupportedRuntime(runtime string) bool {
	// Always return true - the generic builder accepts any runtime value
	// or no runtime at all. Kept for backward compatibility.
	return true
}

