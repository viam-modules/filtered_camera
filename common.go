package filtered_camera

import (
	"context"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/rimage"
)

var Family = resource.ModelNamespace("viam").WithFamily("camera")

func IsFromDataMgmt(ctx context.Context, extra map[string]interface{}) bool {
	if ctx.Value(data.FromDMContextKey{}) == true {
		return true
	}

	if extra != nil && extra[data.FromDMString] == true {
		return true
	}

	return false
}

// Returns a byte array representing the first image in the list of named images
func ImagesToImage(ctx context.Context, ni []camera.NamedImage, mimeType string) ([]byte, camera.ImageMetadata, error) {
	data, err := rimage.EncodeImage(ctx, ni[0].Image, mimeType)
	if err != nil {
		return nil, camera.ImageMetadata{}, err
	}

	return data, camera.ImageMetadata{MimeType: mimeType}, nil
}
