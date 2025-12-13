package filtered_camera

import (
	"context"
	"image"
	"testing"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/test"
)

func TestIsFromDataMgmt(t *testing.T) {
	t.Run("extra with FromDMString true", func(t *testing.T) {
		ctx := context.Background()
		extra := map[string]interface{}{
			data.FromDMString: true,
		}
		result := IsFromDataMgmt(ctx, extra)
		test.That(t, result, test.ShouldBeTrue)
	})

	t.Run("extra with FromDMString false", func(t *testing.T) {
		ctx := context.Background()
		extra := map[string]interface{}{
			data.FromDMString: false,
		}
		result := IsFromDataMgmt(ctx, extra)
		test.That(t, result, test.ShouldBeFalse)
	})

	t.Run("nil extra returns false", func(t *testing.T) {
		ctx := context.Background()
		result := IsFromDataMgmt(ctx, nil)
		test.That(t, result, test.ShouldBeFalse)
	})

	t.Run("empty extra returns false", func(t *testing.T) {
		ctx := context.Background()
		extra := map[string]interface{}{}
		result := IsFromDataMgmt(ctx, extra)
		test.That(t, result, test.ShouldBeFalse)
	})
}

func TestImagesToImage(t *testing.T) {
	t.Run("empty slice returns error", func(t *testing.T) {
		ctx := context.Background()
		emptySlice := []camera.NamedImage{}

		data, metadata, err := ImagesToImage(ctx, emptySlice)
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "NamedImage slice is empty")
		test.That(t, data, test.ShouldBeNil)
		test.That(t, metadata, test.ShouldResemble, camera.ImageMetadata{})
	})

	t.Run("single image returns correctly", func(t *testing.T) {
		ctx := context.Background()

		img := image.NewRGBA(image.Rect(0, 0, 10, 10))
		testImage, err := camera.NamedImageFromImage(img, "test", "image/jpeg", data.Annotations{})
		test.That(t, err, test.ShouldBeNil)

		namedImages := []camera.NamedImage{testImage}

		data, metadata, err := ImagesToImage(ctx, namedImages)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, data, test.ShouldNotBeNil)
		test.That(t, len(data), test.ShouldBeGreaterThan, 0)
		test.That(t, metadata.MimeType, test.ShouldEqual, "image/jpeg")
	})

	t.Run("multiple images returns only first", func(t *testing.T) {
		ctx := context.Background()

		// Create two test images
		img1 := image.NewRGBA(image.Rect(0, 0, 10, 10))
		img2 := image.NewRGBA(image.Rect(0, 0, 20, 20))
		testImage1, err := camera.NamedImageFromImage(img1, "first", "image/jpeg", data.Annotations{})
		test.That(t, err, test.ShouldBeNil)
		testImage2, err := camera.NamedImageFromImage(img2, "second", "image/png", data.Annotations{})
		test.That(t, err, test.ShouldBeNil)

		namedImages := []camera.NamedImage{testImage1, testImage2}

		data, metadata, err := ImagesToImage(ctx, namedImages)
		test.That(t, err, test.ShouldBeNil)
		test.That(t, data, test.ShouldNotBeNil)
		test.That(t, len(data), test.ShouldBeGreaterThan, 0)
		test.That(t, metadata.MimeType, test.ShouldEqual, "image/jpeg") // Should be first image's MIME type
	})
}
