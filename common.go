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
	if ctx.Value(data.FromDMContextKey{}) == true {
		return true
	}

	if extra != nil && extra[data.FromDMString] == true {
		return true
	}

	return false
}

func ImagesToImage(ctx context.Context, ni []camera.NamedImage) ([]byte, camera.ImageMetadata, error) {
	if len(ni) == 0 {
		return nil, camera.ImageMetadata{}, errors.New("NamedImage slice is empty, nothing to turn into an Image")
	}
	bytes, err := ni[0].Bytes(ctx)
	if err != nil {
		return nil, camera.ImageMetadata{}, err
	}

	// Pass in annotations per image.
	// The other option is to add annotations to the NamedImage struct.
	// This is just a placeholder since I don't know where exactly in the
	// pipeline the annotations will be added.
	return bytes, camera.ImageMetadata{
		MimeType: ni[0].MimeType(),
		Annotations: data.Annotations{
			Classifications: []data.Classification{
				{Label: "test_filtered_cam", Confidence: new(float64)},
			},
		}}, nil
}
