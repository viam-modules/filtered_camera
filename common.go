package filtered_camera

import (
	"context"
	"errors"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/resource"
)

var Family = resource.ModelNamespace("viam").WithFamily("camera")

func IsFromDataMgmt(ctx context.Context, extra map[string]interface{}) bool {
	if extra != nil && extra[data.FromDMString] == true {
		return true
	}

	return false
}

func ImagesToImage(ctx context.Context, ni []camera.NamedImage) ([]byte, camera.ImageMetadata, error) {
	if len(ni) == 0 {
		return nil, camera.ImageMetadata{}, errors.New("NamedImage slice is empty, nothing to turn into an Image")
	}
	data, err := ni[0].Bytes(ctx)
	if err != nil {
		return nil, camera.ImageMetadata{}, err
	}

	return data, camera.ImageMetadata{MimeType: ni[0].MimeType(), Annotations: ni[0].Annotations}, nil
}
