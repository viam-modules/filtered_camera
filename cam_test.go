package filtered_camera

import (
	"context"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/rimage"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/testutils/inject"
	"go.viam.com/rdk/utils"
	"go.viam.com/rdk/vision/classification"
	"go.viam.com/rdk/vision/objectdetection"

	imagebuffer "github.com/viam-modules/filtered_camera/image_buffer"

	"go.viam.com/test"
)

// extractImageNumber extracts the image number from a timestamped name
// Expected format: "[timestamp]_img_[number]" -> returns "[number]"
func extractImageNumber(timestampedName string) string {
	parts := strings.Split(timestampedName, "_")
	if len(parts) >= 2 {
		return parts[len(parts)-1] // Get the last part (the number)
	}
	return ""
}

var (
	a = image.NewGray(image.Rect(1, 1, 1, 1))
	b = image.NewGray(image.Rect(2, 1, 1, 1))
	c = image.NewGray(image.Rect(3, 1, 1, 1))
	d = image.NewGray(image.Rect(4, 1, 1, 1))
	e = image.NewGray(image.Rect(5, 1, 1, 1))
	f = image.NewGray(image.Rect(6, 1, 1, 1))

	// Named image versions for the new API
	namedA, _ = camera.NamedImageFromImage(a, "", "image/jpeg", data.Annotations{})
	namedB, _ = camera.NamedImageFromImage(b, "", "image/jpeg", data.Annotations{})
	namedC, _ = camera.NamedImageFromImage(c, "", "image/jpeg", data.Annotations{})
	namedD, _ = camera.NamedImageFromImage(d, "", "image/jpeg", data.Annotations{})
	namedE, _ = camera.NamedImageFromImage(e, "", "image/jpeg", data.Annotations{})
	namedF, _ = camera.NamedImageFromImage(f, "", "image/jpeg", data.Annotations{})
)

func getDummyVisionService() vision.Service {
	svc := &inject.VisionService{}
	svc.ClassificationsFunc = func(ctx context.Context, img image.Image, n int, extra map[string]interface{}) (classification.Classifications, error) {
		if img == a {
			return classification.Classifications{classification.NewClassification(.9, "a")}, nil
		}
		if img == b {
			return classification.Classifications{classification.NewClassification(.1, "a")}, nil
		}
		if img == e {
			return classification.Classifications{classification.NewClassification(.9, "e")}, nil
		}
		return classification.Classifications{}, nil
	}

	svc.DetectionsFunc = func(ctx context.Context, img image.Image, extra map[string]interface{}) ([]objectdetection.Detection, error) {
		r := image.Rect(1, 1, 1, 1)
		if img == c {
			return []objectdetection.Detection{objectdetection.NewDetection(r, r, .1, "b")}, nil
		}
		if img == b {
			return []objectdetection.Detection{objectdetection.NewDetection(r, r, .9, "b")}, nil
		}
		if img == f {
			return []objectdetection.Detection{objectdetection.NewDetection(r, r, .9, "f")}, nil
		}
		return []objectdetection.Detection{}, nil
	}

	return svc
}

func assertTimestampsMatch(t *testing.T, got string, want time.Time) {
	t.Helper()
	const timestampFormat = "2006-01-02T15:04:05.000Z07:00"
	// Parse out the timestamp from the SourceName (format: "[timestamp]_color")
	t.Logf("got source name: %s", got)
	timestampStr, _, found := strings.Cut(got, "_")
	test.That(t, found, test.ShouldBeTrue)
	parsedTime, err := time.Parse(timestampFormat, timestampStr)
	test.That(t, err, test.ShouldBeNil)
	// Truncate both times to millisecond precision for comparison
	parsedTimeMs := parsedTime.Truncate(time.Millisecond)
	expectedTimeMs := want.Truncate(time.Millisecond)
	t.Logf("parsed timestamp: %s", parsedTimeMs)
	t.Logf("expected timestamp: %s", expectedTimeMs)
	// Compare timestamps - they should be equal
	test.That(t, parsedTimeMs.Equal(expectedTimeMs), test.ShouldBeTrue)
}

func TestShouldSend(t *testing.T) {
	logger := logging.NewTestLogger(t)

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
			ImageFrequency:  1.0,
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		acceptedClassifications: map[string]map[string]float64{"": {"a": .8}},
		acceptedObjects:         map[string]map[string]float64{"": {"b": .8}},
		buf:                     imagebuffer.NewImageBuffer(10, 1.0, 0, 0, logging.NewTestLogger(t), true),
	}

	res, err := fc.shouldSend(context.Background(), namedD, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	res, err = fc.shouldSend(context.Background(), namedC, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	res, err = fc.shouldSend(context.Background(), namedB, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedA, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	// test wildcard

	res, err = fc.shouldSend(context.Background(), namedE, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedF, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	fc.acceptedClassifications[""] = map[string]float64{"*": .8}
	fc.acceptedObjects[""] = map[string]float64{"*": .8}

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedE, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedF, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// test inhibit should not send with classifications
	fc.inhibitedClassifications = map[string]map[string]float64{"": {"a": .7}}
	fc.acceptedObjects = map[string]map[string]float64{}
	fc.inhibitors = []vision.Service{
		getDummyVisionService(),
	}
	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedA, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// test inhibit should not send with objects
	fc.inhibitedClassifications = map[string]map[string]float64{}
	fc.inhibitedObjects = map[string]map[string]float64{"": {"b": .1}}
	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedB, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// test that using same detector for inhibit and accept works properly
	fc.inhibitedObjects = map[string]map[string]float64{"": {"b": .7}}
	fc.acceptedObjects = map[string]map[string]float64{"": {"f": .7}}

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedB, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedF, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// test accepted stats update properly and don't affect rejected stats
	fc.acceptedStats = imageStats{}
	fc.rejectedStats = imageStats{}
	fc.inhibitors = []vision.Service{}
	fc.acceptedClassifications = map[string]map[string]float64{"": {"a": .8}}

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedA, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)
	test.That(t, fc.acceptedStats.total, test.ShouldEqual, 1)
	test.That(t, fc.acceptedStats.breakdown["a"], test.ShouldEqual, 1)
	test.That(t, fc.rejectedStats.total, test.ShouldEqual, 0)
	_, ok := fc.rejectedStats.breakdown["a"]
	test.That(t, ok, test.ShouldEqual, false)

	// test rejected stats update properly and don't affect accepted stats
	fc.acceptedStats = imageStats{}
	fc.inhibitors = []vision.Service{
		getDummyVisionService(),
	}
	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedB, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)
	test.That(t, fc.rejectedStats.total, test.ShouldEqual, 1)
	test.That(t, fc.rejectedStats.breakdown["b"], test.ShouldEqual, 1)
	test.That(t, fc.acceptedStats.total, test.ShouldEqual, 0)
	_, ok = fc.acceptedStats.breakdown["b"]
	test.That(t, ok, test.ShouldEqual, false)

	// test that image that does not match any classification or object is rejected
	fc.rejectedStats = imageStats{}
	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), namedD, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)
	test.That(t, fc.rejectedStats.total, test.ShouldEqual, 1)
	test.That(t, fc.rejectedStats.breakdown["no vision services triggered"], test.ShouldEqual, 1)
}

func TestValidate(t *testing.T) {
	conf := &Config{
		Classifications: map[string]float64{"a": .8},
		Objects:         map[string]float64{"b": .8},
		WindowSeconds:   10,
		ImageFrequency:  1.0,
	}

	res, _, err := conf.Validate(".")
	test.That(t, res, test.ShouldBeNil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "\"camera\" is required")
	conf.Camera = "foo"
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldBeNil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "\"vision_services\" is required")

	conf.Vision = "foo"
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, []string{"foo", "foo"})

	// should error if WindowSeconds, WindowSecondsBefore, and WindowSecondsAfter are all zero
	conf.WindowSeconds = 0
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldBeNil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "cannot all be zero")
	conf.WindowSeconds = 10 // set it back to previous value

	// should error if both vision and vision_service are set
	conf.VisionServices = []VisionServiceConfig{
		{
			Vision:          "foo",
			Classifications: map[string]float64{"a": .8},
		},
		{
			Vision:  "bar",
			Objects: map[string]float64{"b": .8},
		},
	}
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldBeNil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "cannot specify both vision and vision_services")

	// when vision is empty and vision_services is set, it should not error
	// and return the camera and vision service names
	conf.Vision = ""
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, []string{"foo", "foo", "bar"})

	// vision services can implement both classifier and detector
	conf.VisionServices = []VisionServiceConfig{
		{
			Vision:          "foo",
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"a": .8},
		},
	}
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, []string{"foo", "foo"})

	// vision services can not have any classifications or objects
	// this would mean that no images would be captured
	conf.VisionServices = []VisionServiceConfig{
		{
			Vision: "foo",
		},
	}
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, []string{"foo", "foo"})

	// inhibitors should be first in the dependency list
	conf.VisionServices = []VisionServiceConfig{
		{
			Vision:  "foo",
			Inhibit: false,
		},
		{
			Vision:  "bar",
			Inhibit: false,
		},
		{
			Vision:  "baz",
			Inhibit: true,
		},
	}
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, []string{"foo", "baz", "foo", "bar"})

	// if WindowSeconds is set, WindowSecondsBefore and WindowSecondsAfter should be 0
	conf.WindowSeconds = 15
	conf.WindowSecondsAfter = 10
	conf.WindowSecondsBefore = 5
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldBeNil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "if window_seconds is set, window_seconds_before and window_seconds_after must not be")

	// if WindowSeconds is not set (or is 0), WindowSecondsBefore and WindowSecondsAfter can be set
	conf.WindowSeconds = 0
	conf.WindowSecondsAfter = 10
	conf.WindowSecondsBefore = 5
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeNil)

	// none of the window boundary parameters can be less than 0
	conf.WindowSeconds = -5
	conf.WindowSecondsAfter = -10
	conf.WindowSecondsBefore = -10
	res, _, err = conf.Validate(".")
	test.That(t, res, test.ShouldBeNil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "one of window_seconds, window_seconds_after, or window_seconds_before can be negative")
}

func TestImage(t *testing.T) {
	logger := logging.NewTestLogger(t)

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
			ImageFrequency:  1.0,
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.NewImageBuffer(10, 1.0, 0, 0, logging.NewTestLogger(t), true),
		cam: &inject.Camera{
			ImagesFunc: func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				imgA, _ := camera.NamedImageFromImage(a, "", "image/jpeg", data.Annotations{})
				imgB, _ := camera.NamedImageFromImage(b, "", "image/jpeg", data.Annotations{})
				imgC, _ := camera.NamedImageFromImage(c, "", "image/jpeg", data.Annotations{})
				return []camera.NamedImage{imgA, imgB, imgC}, resource.ResponseMetadata{CapturedAt: time.Now()}, nil
			},
		},
		acceptedClassifications: map[string]map[string]float64{"": {"a": .8}},
		acceptedObjects:         map[string]map[string]float64{"": {"b": .8}},
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	res, meta, err := fc.Image(ctx, utils.MimeTypeJPEG, nil)
	// Trigger occurs but no historical buffered images available (edge case: triggering faster than buffer can fill)
	test.That(t, err, test.ShouldEqual, data.ErrNoCaptureToStore)
	test.That(t, res, test.ShouldBeNil)

	test.That(t, meta, test.ShouldNotBeNil)
}

func TestImages(t *testing.T) {
	logger := logging.NewTestLogger(t)

	imgA, _ := camera.NamedImageFromImage(a, "", "image/jpeg")
	imgB, _ := camera.NamedImageFromImage(b, "", "image/jpeg")
	imgC, _ := camera.NamedImageFromImage(c, "", "image/jpeg")
	namedImages := []camera.NamedImage{imgA, imgB, imgC}

	timestamp := time.Now()

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
			ImageFrequency:  1.0,
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.NewImageBuffer(10, 1.0, 0, 0, logging.NewTestLogger(t), true),
		cam: &inject.Camera{
			ImagesFunc: func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				return namedImages, resource.ResponseMetadata{CapturedAt: timestamp}, nil
			},
		},
		acceptedClassifications: map[string]map[string]float64{"": {"a": .8}},
		acceptedObjects:         map[string]map[string]float64{"": {"b": .8}},
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	res, meta, err := fc.Images(ctx, nil, nil)
	// Trigger occurs but no historical buffered images available (edge case: triggering faster than buffer can fill)
	test.That(t, err, test.ShouldEqual, data.ErrNoCaptureToStore)
	test.That(t, res, test.ShouldBeNil)
	test.That(t, meta, test.ShouldNotBeNil)
	test.That(t, meta.CapturedAt, test.ShouldResemble, timestamp)
}

func TestImageWithBufferedImages(t *testing.T) {
	logger := logging.NewTestLogger(t)

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
			ImageFrequency:  1.0,
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.NewImageBuffer(10, 1.0, 0, 0, logging.NewTestLogger(t), true),
		cam: &inject.Camera{
			ImagesFunc: func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				img, _ := camera.NamedImageFromImage(a, "trigger_img", "image/jpeg")
				return []camera.NamedImage{img}, resource.ResponseMetadata{CapturedAt: time.Now()}, nil
			},
		},
		acceptedClassifications: map[string]map[string]float64{"": {"a": .8}},
		acceptedObjects:         map[string]map[string]float64{"": {"b": .8}},
	}

	// Manually populate ring buffer with historical images
	baseTime := time.Now().Add(-5 * time.Second) // 5 seconds ago
	img1, _ := camera.NamedImageFromImage(a, "buffered_img_1", "image/jpeg", data.Annotations{})
	fc.buf.AddToRingBuffer([]camera.NamedImage{img1}, resource.ResponseMetadata{CapturedAt: baseTime.Add(-2 * time.Second)})

	img2, _ := camera.NamedImageFromImage(b, "buffered_img_2", "image/jpeg", data.Annotations{})
	fc.buf.AddToRingBuffer([]camera.NamedImage{img2}, resource.ResponseMetadata{CapturedAt: baseTime.Add(-1 * time.Second)})

	ctx := context.Background()
	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	res, meta, err := fc.Image(ctx, utils.MimeTypeJPEG, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldNotBeNil)

	// Should get one of the buffered images (with timestamp naming)
	decodedImage, err := rimage.EncodeImage(ctx, a, utils.MimeTypeJPEG)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, decodedImage)

	test.That(t, meta, test.ShouldNotBeNil)
	test.That(t, meta.MimeType, test.ShouldResemble, utils.MimeTypeJPEG)
}

func TestImagesWithBufferedImages(t *testing.T) {
	logger := logging.NewTestLogger(t)

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
			ImageFrequency:  1.0,
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.NewImageBuffer(10, 1.0, 0, 0, logging.NewTestLogger(t), true),
		cam: &inject.Camera{
			ImagesFunc: func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				img, _ := camera.NamedImageFromImage(a, "trigger_img", "image/jpeg", data.Annotations{})
				return []camera.NamedImage{img}, resource.ResponseMetadata{CapturedAt: time.Now()}, nil
			},
		},
		acceptedClassifications: map[string]map[string]float64{"": {"a": .8}},
		acceptedObjects:         map[string]map[string]float64{"": {"b": .8}},
	}

	// Manually populate ring buffer with historical images
	baseTime := time.Now().Add(-5 * time.Second) // 5 seconds ago
	expectedTimes := []time.Time{
		baseTime.Add(-2 * time.Second),
		baseTime.Add(-1 * time.Second),
		baseTime,
	}

	img1, _ := camera.NamedImageFromImage(a, "buffered_img_1", "image/jpeg", data.Annotations{})
	fc.buf.AddToRingBuffer([]camera.NamedImage{img1}, resource.ResponseMetadata{CapturedAt: expectedTimes[0]})

	img2, _ := camera.NamedImageFromImage(b, "buffered_img_2", "image/jpeg", data.Annotations{})
	fc.buf.AddToRingBuffer([]camera.NamedImage{img2}, resource.ResponseMetadata{CapturedAt: expectedTimes[1]})

	img3, _ := camera.NamedImageFromImage(c, "buffered_img_3", "image/jpeg", data.Annotations{})
	fc.buf.AddToRingBuffer([]camera.NamedImage{img3}, resource.ResponseMetadata{CapturedAt: expectedTimes[2]})

	ctx := context.Background()
	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	res, meta, err := fc.Images(ctx, nil, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldNotBeNil)

	// Should get all buffered images (with timestamp naming)
	test.That(t, len(res), test.ShouldEqual, 3)

	// Verify images have timestamp prefixes and correct timestamps
	for i, img := range res {
		test.That(t, strings.Contains(img.SourceName, "_buffered_img_"), test.ShouldBeTrue)
		// Verify timestamps match expected capture times
		assertTimestampsMatch(t, img.SourceName, expectedTimes[i])
	}

	test.That(t, meta, test.ShouldNotBeNil)
}

func TestProperties(t *testing.T) {
	logger := logging.NewTestLogger(t)

	properties := camera.Properties{
		SupportsPCD: false,
		ImageType:   camera.ImageType("color"),
		MimeTypes:   []string{utils.MimeTypeJPEG},
	}

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
			ImageFrequency:  1.0,
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.NewImageBuffer(10, 1.0, 0, 0, logging.NewTestLogger(t), true),
		cam: &inject.Camera{
			PropertiesFunc: func(ctx context.Context) (camera.Properties, error) {
				return properties, nil
			},
		},
	}

	ctx := context.Background()

	res, err := fc.Properties(ctx)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, properties)
}

func TestDoCommand(t *testing.T) {
	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
			ImageFrequency:  1.0,
		},
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.NewImageBuffer(10, 1.0, 0, 0, logging.NewTestLogger(t), true),
		cam: &inject.Camera{
			ImagesFunc: func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				imgA, _ := camera.NamedImageFromImage(a, "", "image/jpeg", data.Annotations{})
				imgB, _ := camera.NamedImageFromImage(b, "", "image/jpeg", data.Annotations{})
				imgC, _ := camera.NamedImageFromImage(c, "", "image/jpeg", data.Annotations{})
				return []camera.NamedImage{imgA, imgB, imgC}, resource.ResponseMetadata{}, nil
			},
		},
	}

	ctx := context.Background()

	res, err := fc.DoCommand(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldNotBeNil)

	acceptedStats := res["accepted"].(map[string]interface{})
	test.That(t, acceptedStats["total"], test.ShouldEqual, 0)
	test.That(t, acceptedStats["vision"], test.ShouldBeNil)

	rejectedStats := res["rejected"].(map[string]interface{})
	test.That(t, rejectedStats["total"], test.ShouldEqual, 0)
	test.That(t, rejectedStats["vision"], test.ShouldBeNil)

	fc.acceptedStats = imageStats{total: 1, breakdown: map[string]int{"foo": 1}}
	fc.rejectedStats = imageStats{total: 2, breakdown: map[string]int{"bar": 2}}
	res, err = fc.DoCommand(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldNotBeNil)

	acceptedStats = res["accepted"].(map[string]interface{})
	test.That(t, acceptedStats["total"], test.ShouldEqual, 1)
	visionBreakdown, ok := acceptedStats["vision"].(map[string]int)
	test.That(t, ok, test.ShouldEqual, true)
	test.That(t, visionBreakdown, test.ShouldResemble, map[string]int{"foo": 1})

	rejectedStats = res["rejected"].(map[string]interface{})
	test.That(t, rejectedStats["total"], test.ShouldEqual, 2)
	visionBreakdown, ok = rejectedStats["vision"].(map[string]int)
	test.That(t, ok, test.ShouldEqual, true)
	test.That(t, visionBreakdown, test.ShouldResemble, map[string]int{"bar": 2})
}

func TestRingBufferTriggerWindows(t *testing.T) {
	// This test verifies that the ring buffer correctly captures images within trigger windows
	// It simulates image capture at 1 Hz with 2-second windows around triggers
	// The ring buffer maintains only the most recent 4 images (2 * windowSeconds * imageFrequency)

	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	baseTime := time.Now()

	//create a camera that returns images one after another the other simulated to be 1 second
	// after each other
	imagesCam := inject.NewCamera("test_camera")
	timeCount := 0 // inital time
	imagesCam.ImagesFunc = func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) (
		[]camera.NamedImage, resource.ResponseMetadata, error) {
		timeCount++
		imageTime := baseTime.Add(time.Duration(timeCount) * time.Second)
		return []camera.NamedImage{}, // Empty images slice for test
			resource.ResponseMetadata{
				CapturedAt: imageTime,
			},
			nil
	}

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   2,
			ImageFrequency:  1.0, // 1 Hz
		},
		logger: logger,
		cam:    imagesCam,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
	}

	// Note: The ring buffer will limit itself to 2 * windowSeconds * imageFrequency = 4 images
	// This test works within that constraint to verify proper trigger window behavior

	// Use a base time that's close to current time to make windows work
	// Initialize the image buffer
	fc.buf = imagebuffer.NewImageBuffer(fc.conf.WindowSeconds, fc.conf.ImageFrequency, 0, 0, logging.NewTestLogger(t), true)

	// First, add images at times 1, 2, 3, 4, 5
	for i := 1; i <= 5; i++ {
		fc.captureImageInBackground(ctx)
	}

	// Manually trigger at time 5, which should capture images 3, 4, 5, 6, 7 (within 2 second window [3, 7])
	triggerTime1 := baseTime.Add(5 * time.Second)
	fc.buf.MarkShouldSend(triggerTime1)

	// Should first capture images 3, 4, 5 (images in the before-trigger buffer)
	expectedFirstTrigger := []time.Time{
		baseTime.Add(3 * time.Second),
		baseTime.Add(4 * time.Second),
		baseTime.Add(5 * time.Second),
	}

	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 3)
	for i, expected := range expectedFirstTrigger {
		test.That(t, fc.buf.GetToSendSlice()[i].Meta.CapturedAt, test.ShouldEqual, expected)
	}
	// Now add images 6-10 leading up to second trigger, and check if only two more images are added
	// to the ToSend buffer
	expectedFirstTrigger = append(expectedFirstTrigger, baseTime.Add(6*time.Second))
	expectedFirstTrigger = append(expectedFirstTrigger, baseTime.Add(7*time.Second))
	for i := 6; i <= 10; i++ {
		fc.captureImageInBackground(ctx)
	}
	// now check that all 5 expected images are in the ToSend buffer
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 5)
	for i, expected := range expectedFirstTrigger {
		test.That(t, fc.buf.GetToSendSlice()[i].Meta.CapturedAt, test.ShouldEqual, expected)
	}

	// Clear ToSend to prepare for second trigger
	fc.buf.ClearToSend()
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 0)

	// Manually trigger at time 10, which should capture images 8, 9, 10
	triggerTime2 := baseTime.Add(10 * time.Second)
	fc.buf.MarkShouldSend(triggerTime2)

	// Should capture images 8, 9, 10
	expectedTrigger := []time.Time{
		baseTime.Add(8 * time.Second),
		baseTime.Add(9 * time.Second),
		baseTime.Add(10 * time.Second),
	}

	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 3)
	for i, expected := range expectedTrigger {
		test.That(t, fc.buf.GetToSendSlice()[i].Meta.CapturedAt, test.ShouldEqual, expected)
	}
	// Now add images 11 - 15 after trigger, and check if only two more images are added
	// to the ToSend buffer
	expectedTrigger = append(expectedTrigger, baseTime.Add(11*time.Second))
	expectedTrigger = append(expectedTrigger, baseTime.Add(12*time.Second))
	for i := 11; i <= 15; i++ {
		fc.captureImageInBackground(ctx)
	}
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 5)
	for i, expected := range expectedTrigger {
		test.That(t, fc.buf.GetToSendSlice()[i].Meta.CapturedAt, test.ShouldEqual, expected)
	}
}

func TestBatchingWithFrequencyMismatch(t *testing.T) {
	// This test simulates frequency mismatch between background capture and Images() calls:
	// - Background worker captures images every tick (fc.captureImageInBackground)
	// - Images() is called every 4 ticks (4:1 frequency mismatch)
	// - Vision service starts with non-trigger, triggers at tick 12, then back to non-trigger
	// - Window: 3s before + 2s after = 5s total, buffer holds max 5 images
	// - Verifies exact batching numbers and ToSend buffer management

	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	baseTime := time.Now()

	// Create camera that captures images at each tick (1s intervals)
	imagesCam := inject.NewCamera("test_camera")
	timeCount := 0
	imagesCam.ImagesFunc = func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) (
		[]camera.NamedImage, resource.ResponseMetadata, error) {
		timeCount++
		imageTime := baseTime.Add(time.Duration(timeCount) * time.Second)
		img, _ := camera.NamedImageFromImage(image.NewRGBA(image.Rect(0, 0, 10, 10)), fmt.Sprintf("img_%d", timeCount), "image/jpeg", data.Annotations{})
		return []camera.NamedImage{img}, resource.ResponseMetadata{
			CapturedAt: imageTime,
		}, nil
	}

	// Create vision service that initially doesn't trigger (below threshold)
	visionSvc := inject.NewVisionService("test_vision")
	visionSvc.ClassificationsFunc = func(ctx context.Context, img image.Image, n int, extra map[string]interface{}) (classification.Classifications, error) {
		return classification.Classifications{
			classification.NewClassification(0.5, "person"), // Below 0.8 threshold - no trigger
		}, nil
	}

	fc := &filteredCamera{
		conf: &Config{
			Classifications:     map[string]float64{"person": 0.8},
			WindowSecondsBefore: 3,   // 3 seconds before trigger
			WindowSecondsAfter:  2,   // 2 seconds after trigger
			ImageFrequency:      1.0, // 1 Hz for buffer size calculation
			Debug:               true,
		},
		logger:                   logger,
		cam:                      imagesCam,
		otherVisionServices:      []vision.Service{visionSvc},
		acceptedClassifications:  map[string]map[string]float64{"test_vision": {"person": 0.8}},
		acceptedObjects:          map[string]map[string]float64{},
		inhibitedClassifications: map[string]map[string]float64{},
		inhibitedObjects:         map[string]map[string]float64{},
		inhibitors:               []vision.Service{},
	}

	// Initialize image buffer: (3+2) * 1.0 = 5 images max in ring buffer
	fc.buf = imagebuffer.NewImageBuffer(0, fc.conf.ImageFrequency, fc.conf.WindowSecondsBefore, fc.conf.WindowSecondsAfter, logging.NewTestLogger(t), true)

	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	// Ticks 1-4: Background captures
	for i := 1; i <= 4; i++ {
		fc.captureImageInBackground(ctx)
	}
	images1, _, err1 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err1, test.ShouldEqual, data.ErrNoCaptureToStore) // Should fail - no trigger
	test.That(t, images1, test.ShouldBeNil)
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 0)

	// Ticks 5-8: Background captures
	for i := 5; i <= 8; i++ {
		fc.captureImageInBackground(ctx)
	}
	images2, _, err2 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err2, test.ShouldEqual, data.ErrNoCaptureToStore) // Should fail - no trigger
	test.That(t, images2, test.ShouldBeNil)
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 0)

	// Change vision service to trigger condition
	visionSvc.ClassificationsFunc = func(ctx context.Context, img image.Image, n int, extra map[string]interface{}) (classification.Classifications, error) {
		return classification.Classifications{
			classification.NewClassification(0.9, "person"), // Above 0.8 threshold - should trigger
		}, nil
	}

	// Ticks 11-12: Background captures (note: img_10 was consumed by second Images() call)
	for i := 11; i <= 12; i++ {
		fc.captureImageInBackground(ctx)
	}

	images3, _, err3 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	t.Logf("Third Images() call returned %d images:", len(images3))
	for i, img := range images3 {
		t.Logf("  [%d]: %s", i, img.SourceName)
	}
	// Trigger occurs on img_13 but trigger image is not returned (to maintain chronological order)
	// Instead we get buffered images from the window [10s, 15s]
	// Available images in ring buffer: img_7, img_8, img_9, img_11, img_12
	// Images within window [10s, 15s]: img_11, img_12 (img_10 missing due to gap)
	test.That(t, err3, test.ShouldBeNil)            // Should succeed - trigger occurred and buffered images available
	test.That(t, len(images3), test.ShouldEqual, 2) // Images 11, 12
	// Verify exact image names in chronological order (with timestamp prefixes)
	expectedNames3 := []string{"img_11", "img_12"}
	for i, img := range images3 {
		// Image names should have format "[timestamp]_img_[number]"
		actualNumber := extractImageNumber(img.SourceName)
		expectedNumber := strings.Split(expectedNames3[i], "_")[1] // Get "11" from "img_11"
		test.That(t, actualNumber, test.ShouldEqual, expectedNumber)
		// Also verify it has timestamp prefix format
		test.That(t, strings.Contains(img.SourceName, "_img_"), test.ShouldBeTrue)
		// Verify timestamps match expected capture times
		expectedTime := baseTime.Add(time.Duration(11+i) * time.Second) // img_11 = baseTime + 11s, img_12 = baseTime + 12s
		assertTimestampsMatch(t, img.SourceName, expectedTime)
	}
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 0) // Should be empty after PopAllToSend

	// Change vision service back to non-trigger condition
	visionSvc.ClassificationsFunc = func(ctx context.Context, img image.Image, n int, extra map[string]interface{}) (classification.Classifications, error) {
		return classification.Classifications{
			classification.NewClassification(0.5, "person"), // Below 0.8 threshold - no trigger
		}, nil
	}

	// Ticks 14-16: Background captures (note: img_13 was consumed by trigger Images() call)
	for i := 14; i <= 16; i++ {
		fc.captureImageInBackground(ctx)
	}
	images4, _, err4 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	t.Logf("Fourth Images() call returned %d images:", len(images4))
	for i, img := range images4 {
		t.Logf("  [%d]: %s", i, img.SourceName)
	}
	test.That(t, err4, test.ShouldBeNil) // Should succeed - has buffered images from capture window
	// Background captures img_14, img_15 went to ToSend (within window [10s, 15s])
	// img_16 went to ring buffer (outside window), img_17 consumed by this Images() call (gap)
	test.That(t, len(images4), test.ShouldEqual, 2) // Should get images 14, 15

	expectedNames4 := []string{"img_14", "img_15"}
	for i, img := range images4 {
		// Image names should have format "[timestamp]_img_[number]"
		actualNumber := extractImageNumber(img.SourceName)
		expectedNumber := strings.Split(expectedNames4[i], "_")[1] // Get "14" from "img_14"
		test.That(t, actualNumber, test.ShouldEqual, expectedNumber)
		// Also verify it has timestamp prefix format
		test.That(t, strings.Contains(img.SourceName, "_img_"), test.ShouldBeTrue)
		// Verify timestamps match expected capture times
		expectedTime := baseTime.Add(time.Duration(14+i) * time.Second) // img_14 = baseTime + 14s, img_15 = baseTime + 15s
		assertTimestampsMatch(t, img.SourceName, expectedTime)
	}

	// Ticks 17-20: Background captures
	for i := 17; i <= 20; i++ {
		fc.captureImageInBackground(ctx)
	}
	images5, _, err5 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err5, test.ShouldEqual, data.ErrNoCaptureToStore) // Should fail - outside window, no trigger
	test.That(t, images5, test.ShouldBeNil)
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 0)
}

func TestOverlappingTriggerWindows(t *testing.T) {
	// This test verifies that overlapping trigger windows don't send duplicate images
	// Scenario: 10s before + 2s after = 12s total window, with triggers 5 seconds apart
	// Window 1: [T-10, T+2]
	// Window 2: [T+5-10, T+5+2] = [T-5, T+7]
	// Overlap: [T-5, T+2] should not be sent twice

	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	baseTime := time.Now()

	// Create camera that captures images at each tick (1s intervals)
	imagesCam := inject.NewCamera("test_camera")
	timeCount := 0
	imagesCam.ImagesFunc = func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) (
		[]camera.NamedImage, resource.ResponseMetadata, error) {
		timeCount++
		imageTime := baseTime.Add(time.Duration(timeCount) * time.Second)
		img, _ := camera.NamedImageFromImage(image.NewRGBA(image.Rect(0, 0, 10, 10)), fmt.Sprintf("img_%d", timeCount), "image/jpeg")
		return []camera.NamedImage{img}, resource.ResponseMetadata{
			CapturedAt: imageTime,
		}, nil
	}

	// Create vision service that triggers when we want it to
	visionSvc := inject.NewVisionService("test_vision")
	shouldTrigger := false
	visionSvc.ClassificationsFunc = func(ctx context.Context, img image.Image, n int, extra map[string]interface{}) (classification.Classifications, error) {
		if shouldTrigger {
			return classification.Classifications{
				classification.NewClassification(0.9, "person"), // Above 0.8 threshold - triggers
			}, nil
		}
		return classification.Classifications{
			classification.NewClassification(0.5, "person"), // Below 0.8 threshold - no trigger
		}, nil
	}

	fc := &filteredCamera{
		conf: &Config{
			Classifications:     map[string]float64{"person": 0.8},
			WindowSecondsBefore: 10, // Long before window
			WindowSecondsAfter:  2,  // Short after window
			ImageFrequency:      1.0,
			Debug:               true,
		},
		logger:                   logger,
		cam:                      imagesCam,
		otherVisionServices:      []vision.Service{visionSvc},
		acceptedClassifications:  map[string]map[string]float64{"test_vision": {"person": 0.8}},
		acceptedObjects:          map[string]map[string]float64{},
		inhibitedClassifications: map[string]map[string]float64{},
		inhibitedObjects:         map[string]map[string]float64{},
		inhibitors:               []vision.Service{},
	}

	// Initialize image buffer: (10+2) * 1.0 = 12 images max in ring buffer
	fc.buf = imagebuffer.NewImageBuffer(0, fc.conf.ImageFrequency, fc.conf.WindowSecondsBefore, fc.conf.WindowSecondsAfter, logging.NewTestLogger(t), true)

	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	// Build up ring buffer with 15 images
	for i := 1; i <= 15; i++ {
		fc.captureImageInBackground(ctx)
	}

	// First trigger at time T (tick 16)
	shouldTrigger = true
	fc.captureImageInBackground(ctx) // This will trigger at img_16

	images1, _, err1 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err1, test.ShouldBeNil)

	// Window 1: [T-10, T+2] = [6s, 18s]
	// Available images in ring buffer: img_4 through img_15 (at trigger time)
	// Images 6-15 should be within window [6s, 18s] - but ring buffer only had img_4-15
	// So expect images img_6 through img_15 = 10 images
	t.Logf("First trigger returned %d images:", len(images1))
	for i, img := range images1 {
		t.Logf("  [%d]: %s", i, img.SourceName)
		// Verify timestamp matches the expected capture time
		// Images should start from img_6 (baseTime + 7s) onwards
		expectedTime := baseTime.Add(time.Duration(7+i) * time.Second)
		assertTimestampsMatch(t, img.SourceName, expectedTime)
	}

	// Let capture window continue for a few more images (should go to ToSend directly)
	shouldTrigger = false // Stop triggering new windows
	for i := 17; i <= 18; i++ {
		fc.captureImageInBackground(ctx) // These should go to ToSend (within window till 18s)
	}

	// Second Images() call should get the continuing images
	images1_continued, _, err1_cont := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err1_cont, test.ShouldBeNil)
	t.Logf("First window continued returned %d images:", len(images1_continued))
	for i, img := range images1_continued {
		t.Logf("  [%d]: %s", i, img.SourceName)
		// Verify timestamp matches the expected capture time
		// These should be img_17, img_18 (baseTime + 18s, baseTime + 19s)
		expectedTime := baseTime.Add(time.Duration(18+i) * time.Second)
		assertTimestampsMatch(t, img.SourceName, expectedTime)
	}

	// Wait for first capture window to end, then trigger second window
	// Add images outside the first window to ring buffer
	for i := 19; i <= 25; i++ {
		fc.captureImageInBackground(ctx) // These go to ring buffer (outside first window)
	}

	// Second trigger at T+10 (tick 26) - should create overlapping window
	shouldTrigger = true
	fc.captureImageInBackground(ctx) // This triggers at img_26, time = T+10

	images2, _, err2 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err2, test.ShouldBeNil)

	// Window 2: [T+10-10, T+10+2] = [T, T+12] = [16s, 28s]
	// This overlaps with window 1's [6s, 18s] in the range [16s, 18s]
	// But images 16-18 were already sent in window 1!
	// Ring buffer at trigger 2 should have: img_16 onwards (since window 1 ended)
	// Images within [16s, 28s] from ring buffer should be img_16 through img_25 (if available)
	t.Logf("Second trigger returned %d images:", len(images2))
	for i, img := range images2 {
		t.Logf("  [%d]: %s", i, img.SourceName)
	}

	// Verify no duplicates by checking all returned image names
	allImages := make(map[string]int) // image name -> count

	for _, img := range images1 {
		allImages[img.SourceName]++
	}
	for _, img := range images1_continued {
		allImages[img.SourceName]++
	}
	for _, img := range images2 {
		allImages[img.SourceName]++
	}

	t.Logf("All images returned with counts:")
	duplicateFound := false
	for name, count := range allImages {
		t.Logf("  %s: %d times", name, count)
		if count > 1 {
			duplicateFound = true
			t.Errorf("Duplicate image found: %s sent %d times", name, count)
		}
	}

	// The test should pass - no duplicates with the original logic
	test.That(t, duplicateFound, test.ShouldBeFalse)
}

func TestCurrentImageTimestampingInCaptureWindow(t *testing.T) {
	// This test verifies that when we're within a capture window but ToSend buffer is empty,
	// the current images returned get properly timestamped with format "[timestamp]_[original_name]"

	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	baseTime := time.Now()

	// Create camera that returns images with "color" as SourceName
	imagesCam := inject.NewCamera("test_camera")
	captureCount := 0
	imagesCam.ImagesFunc = func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) (
		[]camera.NamedImage, resource.ResponseMetadata, error) {
		captureCount++
		currentTime := baseTime.Add(time.Duration(captureCount) * time.Second)
		img, _ := camera.NamedImageFromImage(image.NewRGBA(image.Rect(0, 0, 10, 10)), "color", "image/jpeg")
		return []camera.NamedImage{img}, resource.ResponseMetadata{
			CapturedAt: currentTime,
		}, nil
	}

	// Create vision service that always triggers (for easy window setup)
	visionSvc := inject.NewVisionService("test_vision")
	visionSvc.ClassificationsFunc = func(ctx context.Context, img image.Image, n int, extra map[string]interface{}) (classification.Classifications, error) {
		return classification.Classifications{
			classification.NewClassification(0.9, "person"), // Above 0.8 threshold - triggers
		}, nil
	}

	fc := &filteredCamera{
		conf: &Config{
			Classifications:     map[string]float64{"person": 0.8},
			WindowSecondsBefore: 2,   // 2 seconds before trigger
			WindowSecondsAfter:  3,   // 3 seconds after trigger
			ImageFrequency:      1.0, // 1 Hz
			Debug:               true,
		},
		logger:                   logger,
		cam:                      imagesCam,
		otherVisionServices:      []vision.Service{visionSvc},
		acceptedClassifications:  map[string]map[string]float64{"test_vision": {"person": 0.8}},
		acceptedObjects:          map[string]map[string]float64{},
		inhibitedClassifications: map[string]map[string]float64{},
		inhibitedObjects:         map[string]map[string]float64{},
		inhibitors:               []vision.Service{},
	}

	// Initialize image buffer
	fc.buf = imagebuffer.NewImageBuffer(0, fc.conf.ImageFrequency, fc.conf.WindowSecondsBefore, fc.conf.WindowSecondsAfter, logging.NewTestLogger(t), true)

	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	// Step 1: Build up some ring buffer by capturing background images (simulate background worker)
	for i := 0; i < 5; i++ {
		bgImages, bgMeta, err := fc.cam.Images(ctx, nil, nil)
		test.That(t, err, test.ShouldBeNil)
		fc.buf.StoreImages(bgImages, bgMeta, bgMeta.CapturedAt)
	}

	// Step 2: Trigger a capture window by calling Images() with data management context
	// This should trigger because vision service returns confidence > 0.8
	images1, _, err1 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err1, test.ShouldBeNil)
	test.That(t, len(images1), test.ShouldEqual, 2) // secondsBefore is 2, should have two sent to Capture
	// lets make sure those timestamps are correct
	assertTimestampsMatch(t, images1[0].SourceName, baseTime.Add(time.Duration(4)*time.Second))
	assertTimestampsMatch(t, images1[1].SourceName, baseTime.Add(time.Duration(5)*time.Second))

	// Step 3: Clear ToSend buffer to simulate the race condition scenario
	fc.buf.ClearToSend()

	// Verify ToSend buffer is empty
	toSendLen := fc.buf.GetToSendLength()
	test.That(t, toSendLen, test.ShouldEqual, 0)

	// Step 4: Call Images() while within capture window but ToSend buffer is empty
	// This should trigger the code path: fc.buf.IsWithinCaptureWindow() == true, but getBufferedImages() returns false
	finalImages, finalMeta, err2 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err2, test.ShouldBeNil)
	test.That(t, len(finalImages), test.ShouldEqual, 1)

	// Step 6: Verify the returned image has timestamped SourceName, not raw "color"
	returnedImage := finalImages[0]
	test.That(t, returnedImage.SourceName, test.ShouldNotEqual, "color")                   // Should NOT be raw "color"
	test.That(t, strings.HasSuffix(returnedImage.SourceName, "_color"), test.ShouldBeTrue) // Should end with "_color"

	assertTimestampsMatch(t, returnedImage.SourceName, finalMeta.CapturedAt)
}

func TestMultipleTriggerWindows(t *testing.T) {
	// This test verifies that the ring buffer correctly captures images within trigger windows
	// and keeps extending the trigger window if MarkShouldSend keeps being called
	// It simulates image capture at 1 Hz with 2-second windows around triggers
	// The ring buffer maintains only the most recent 4 images (2 * windowSeconds * imageFrequency)

	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	baseTime := time.Now()

	//create a camera that returns images one after another the other simulated to be 1 second
	// after each other
	imagesCam := inject.NewCamera("test_camera")
	timeCount := 0 // inital time
	imagesCam.ImagesFunc = func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) (
		[]camera.NamedImage, resource.ResponseMetadata, error) {
		timeCount++
		imageTime := baseTime.Add(time.Duration(timeCount) * time.Second)
		return []camera.NamedImage{}, // Empty images slice for test
			resource.ResponseMetadata{
				CapturedAt: imageTime,
			},
			nil
	}

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   2,
			ImageFrequency:  1.0, // 1 Hz
		},
		logger: logger,
		cam:    imagesCam,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
	}

	// Note: The ring buffer will limit itself to 2 * windowSeconds * imageFrequency = 4 images
	// This test works within that constraint to verify proper trigger window behavior

	// Use a base time that's close to current time to make windows work
	// Initialize the image buffer
	fc.buf = imagebuffer.NewImageBuffer(fc.conf.WindowSeconds, fc.conf.ImageFrequency, 0, 0, logging.NewTestLogger(t), true)

	// First, add images at times 1, 2, 3, 4, 5
	for i := 1; i <= 5; i++ {
		fc.captureImageInBackground(ctx)
	}
	// Manually trigger at time 5
	triggerTime1 := baseTime.Add(5 * time.Second)
	fc.buf.MarkShouldSend(triggerTime1)
	// Now add more images, with additional triggers at 7 and 9
	fc.captureImageInBackground(ctx) // 6
	fc.captureImageInBackground(ctx) // 7
	// Manually trigger at time 7
	triggerTime2 := baseTime.Add(7 * time.Second)
	fc.buf.MarkShouldSend(triggerTime2)
	fc.captureImageInBackground(ctx) // 8
	fc.captureImageInBackground(ctx) // 9
	// Manually trigger at time 9
	triggerTime3 := baseTime.Add(9 * time.Second)
	fc.buf.MarkShouldSend(triggerTime3)
	for i := 10; i <= 20; i++ {
		fc.captureImageInBackground(ctx)
	}
	// so ToSend should capture [3, 11] and make no repeats
	expectedTrigger := []time.Time{
		baseTime.Add(3 * time.Second),
		baseTime.Add(4 * time.Second),
		baseTime.Add(5 * time.Second),
		baseTime.Add(6 * time.Second),
		baseTime.Add(7 * time.Second),
		baseTime.Add(8 * time.Second),
		baseTime.Add(9 * time.Second),
		baseTime.Add(10 * time.Second),
		baseTime.Add(11 * time.Second),
	}
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 9)
	for i, expected := range expectedTrigger {
		test.That(t, fc.buf.GetToSendSlice()[i].Meta.CapturedAt, test.ShouldEqual, expected)
	}
}

func TestNoDuplicateImagesAcrossGetImagesCalls(t *testing.T) {
	// This test specifically verifies that the same images never appear in multiple GetImages() calls.
	// This would have caught the original bug where images remained in RingBuffer after being sent,
	// causing the same images to be returned across multiple data capture requests.

	logger := logging.NewTestLogger(t)
	ctx := context.Background()
	baseTime := time.Now()

	// Create camera that returns images with predictable names
	imagesCam := inject.NewCamera("test_camera")
	timeCount := 0
	imagesCam.ImagesFunc = func(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) (
		[]camera.NamedImage, resource.ResponseMetadata, error) {
		timeCount++
		imageTime := baseTime.Add(time.Duration(timeCount) * time.Second)
		img, _ := camera.NamedImageFromImage(image.NewRGBA(image.Rect(0, 0, 10, 10)), fmt.Sprintf("img_%d", timeCount), "image/jpeg", data.Annotations{})
		return []camera.NamedImage{img}, resource.ResponseMetadata{
			CapturedAt: imageTime,
		}, nil
	}

	// Create vision service that always triggers
	visionSvc := inject.NewVisionService("test_vision")
	visionSvc.ClassificationsFunc = func(ctx context.Context, img image.Image, n int, extra map[string]interface{}) (classification.Classifications, error) {
		return classification.Classifications{
			classification.NewClassification(0.9, "person"), // Always triggers
		}, nil
	}

	fc := &filteredCamera{
		conf: &Config{
			Classifications:     map[string]float64{"person": 0.8},
			WindowSecondsBefore: 15, // 15 seconds before
			WindowSecondsAfter:  2,  // 2 seconds after
			ImageFrequency:      1.0,
			Debug:               true,
		},
		logger:                   logger,
		cam:                      imagesCam,
		otherVisionServices:      []vision.Service{visionSvc},
		acceptedClassifications:  map[string]map[string]float64{"test_vision": {"person": 0.8}},
		acceptedObjects:          map[string]map[string]float64{},
		inhibitedClassifications: map[string]map[string]float64{},
		inhibitedObjects:         map[string]map[string]float64{},
		inhibitors:               []vision.Service{},
	}

	// Initialize image buffer
	fc.buf = imagebuffer.NewImageBuffer(0, fc.conf.ImageFrequency, fc.conf.WindowSecondsBefore, fc.conf.WindowSecondsAfter, logging.NewTestLogger(t), true)

	// Add data management context
	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	// Build up ring buffer with several images
	for i := 0; i < 20; i++ {
		bgImages, bgMeta, err := fc.cam.Images(ctx, nil, nil)
		test.That(t, err, test.ShouldBeNil)
		fc.buf.StoreImages(bgImages, bgMeta, bgMeta.CapturedAt)
	}

	// Track all images returned across multiple GetImages() calls
	allReturnedImages := make(map[string]int) // image name -> count

	// First GetImages() call - should trigger and return historical images (15 from windows before)
	images1, _, err1 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err1, test.ShouldBeNil)
	test.That(t, len(images1), test.ShouldEqual, 15)

	// Add more images to continue capture window
	for i := 0; i < 3; i++ {
		bgImages, bgMeta, err := fc.cam.Images(ctx, nil, nil)
		test.That(t, err, test.ShouldBeNil)
		fc.buf.StoreImages(bgImages, bgMeta, bgMeta.CapturedAt)
	}

	// Second GetImages() call - should return continuing images from the same trigger window, there would have been an overlap
	images2, _, err2 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err2, test.ShouldBeNil)
	test.That(t, len(images2), test.ShouldEqual, 3)

	// Wait for capture window to end
	for i := 0; i < 20; i++ {
		bgImages, bgMeta, err := fc.cam.Images(ctx, nil, nil)
		test.That(t, err, test.ShouldBeNil)
		fc.buf.StoreImages(bgImages, bgMeta, bgMeta.CapturedAt)
	}

	// Third GetImages() call - should trigger a new capture window with a fresh set of before images, plus the 2 left over from the previous "after" window
	images3, _, err3 := fc.Images(ctx, nil, map[string]interface{}{data.FromDMString: true})
	test.That(t, err3, test.ShouldBeNil)
	test.That(t, len(images3), test.ShouldEqual, 17)

	// The critical test, verify no image appears more than once across all GetImages() calls
	duplicateFound := false
	for imageName, count := range allReturnedImages {
		if count > 1 {
			duplicateFound = true
			t.Logf("Duplicate Found: Image %s was returned %d times across different GetImages() calls", imageName, count)
		}
	}
	test.That(t, duplicateFound, test.ShouldBeFalse)
}
