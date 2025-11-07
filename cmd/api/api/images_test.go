package api

import (
	"testing"
	"time"

	"github.com/onkernel/hypeman/lib/images"
	"github.com/onkernel/hypeman/lib/oapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListImages_Empty(t *testing.T) {
	svc := newTestService(t)

	resp, err := svc.ListImages(ctx(), oapi.ListImagesRequestObject{})
	require.NoError(t, err)

	list, ok := resp.(oapi.ListImages200JSONResponse)
	require.True(t, ok, "expected 200 response")
	assert.Empty(t, list)
}

func TestGetImage_NotFound(t *testing.T) {
	svc := newTestService(t)

	resp, err := svc.GetImage(ctx(), oapi.GetImageRequestObject{
		Id: "non-existent",
	})
	require.NoError(t, err)

	notFound, ok := resp.(oapi.GetImage404JSONResponse)
	require.True(t, ok, "expected 404 response")
	assert.Equal(t, "not_found", notFound.Code)
	assert.Equal(t, "image not found", notFound.Message)
}

func TestCreateImage_Async(t *testing.T) {
	svc := newTestService(t)
	ctx := ctx()

	t.Log("Creating image...")
	createResp, err := svc.CreateImage(ctx, oapi.CreateImageRequestObject{
		Body: &oapi.CreateImageRequest{
			Name: "docker.io/library/alpine:latest",
		},
	})
	require.NoError(t, err)

	acceptedResp, ok := createResp.(oapi.CreateImage202JSONResponse)
	require.True(t, ok, "expected 202 accepted response")

	img := oapi.Image(acceptedResp)
	require.Equal(t, "docker.io/library/alpine:latest", img.Name)
	require.Equal(t, "img-alpine-latest", img.Id)
	t.Logf("Image created: id=%s, initial_status=%s", img.Id, img.Status)

	// Poll until ready
	t.Log("Polling for completion...")
	for i := 0; i < 300; i++ {
		getResp, err := svc.GetImage(ctx, oapi.GetImageRequestObject{Id: img.Id})
		require.NoError(t, err)

		imgResp, ok := getResp.(oapi.GetImage200JSONResponse)
		require.True(t, ok, "expected 200 response")

		currentImg := oapi.Image(imgResp)
		
		if i%10 == 0 || currentImg.Status != img.Status {
			t.Logf("Poll #%d: status=%s, queue_position=%v, has_size=%v", 
				i+1, currentImg.Status, currentImg.QueuePosition, currentImg.SizeBytes != nil)
		}

		if currentImg.Status == oapi.ImageStatus(images.StatusReady) {
			t.Log("Build complete!")
			require.NotNil(t, currentImg.SizeBytes)
			require.Greater(t, *currentImg.SizeBytes, int64(0))
			require.Nil(t, currentImg.Error)
			return
		}

		if currentImg.Status == oapi.ImageStatus(images.StatusFailed) {
			errMsg := ""
			if currentImg.Error != nil {
				errMsg = *currentImg.Error
			}
			t.Fatalf("Build failed: %s", errMsg)
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatal("Build did not complete within 30 seconds")
}

