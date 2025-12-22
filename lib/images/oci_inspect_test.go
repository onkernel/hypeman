package images

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/require"
)

func TestInspectManifestReturnsPlatformSpecificDigestForMultiArch(t *testing.T) {
	// alpine:latest is a multi-arch image on Docker Hub; we should resolve the
	// digest of the platform-specific manifest that we will actually pull.
	const imageRef = "docker.io/library/alpine:latest"

	client, err := newOCIClient(t.TempDir())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	gotDigest, err := client.inspectManifest(ctx, imageRef)
	require.NoError(t, err)
	require.NotEmpty(t, gotDigest)

	ref, err := name.ParseReference(imageRef)
	require.NoError(t, err)

	img, err := remote.Image(ref,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithPlatform(currentPlatform()))
	require.NoError(t, err)

	want, err := img.Digest()
	require.NoError(t, err)

	require.Equal(t, want.String(), gotDigest)
}

