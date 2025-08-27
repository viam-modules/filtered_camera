package filtered_camera

import (
	"context"
	"fmt"
	"image"
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

var (
	a = image.NewGray(image.Rect(1, 1, 1, 1))
	b = image.NewGray(image.Rect(2, 1, 1, 1))
	c = image.NewGray(image.Rect(3, 1, 1, 1))
	d = image.NewGray(image.Rect(4, 1, 1, 1))
	e = image.NewGray(image.Rect(5, 1, 1, 1))
	f = image.NewGray(image.Rect(6, 1, 1, 1))
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

	res, err := fc.shouldSend(context.Background(), d, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	res, err = fc.shouldSend(context.Background(), c, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	res, err = fc.shouldSend(context.Background(), b, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), a, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	// test wildcard

	res, err = fc.shouldSend(context.Background(), e, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), f, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	fc.acceptedClassifications[""] = map[string]float64{"*": .8}
	fc.acceptedObjects[""] = map[string]float64{"*": .8}

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), e, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), f, time.Now())
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

	res, err = fc.shouldSend(context.Background(), a, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// test inhibit should not send with objects
	fc.inhibitedClassifications = map[string]map[string]float64{}
	fc.inhibitedObjects = map[string]map[string]float64{"": {"b": .1}}
	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), b, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// test that using same detector for inhibit and accept works properly
	fc.inhibitedObjects = map[string]map[string]float64{"": {"b": .7}}
	fc.acceptedObjects = map[string]map[string]float64{"": {"f": .7}}

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), b, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), f, time.Now())
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// test accepted stats update properly and don't affect rejected stats
	fc.acceptedStats = imageStats{}
	fc.rejectedStats = imageStats{}
	fc.inhibitors = []vision.Service{}
	fc.acceptedClassifications = map[string]map[string]float64{"": {"a": .8}}

	// Reset buffer state to clear CaptureTill
	fc.buf.SetCaptureTill(time.Time{})

	res, err = fc.shouldSend(context.Background(), a, time.Now())
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

	res, err = fc.shouldSend(context.Background(), b, time.Now())
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

	res, err = fc.shouldSend(context.Background(), d, time.Now())
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
			ImagesFunc: func(ctx context.Context, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				return []camera.NamedImage{
					{Image: a, SourceName: ""},
					{Image: b, SourceName: ""},
					{Image: c, SourceName: ""},
				}, resource.ResponseMetadata{CapturedAt: time.Now()}, nil
			},
		},
		acceptedClassifications: map[string]map[string]float64{"": {"a": .8}},
		acceptedObjects:         map[string]map[string]float64{"": {"b": .8}},
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	res, meta, err := fc.Image(ctx, utils.MimeTypeJPEG, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldNotBeNil)

	decodedImage, err := rimage.EncodeImage(ctx, a, utils.MimeTypeJPEG)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, decodedImage)

	test.That(t, meta, test.ShouldNotBeNil)
	test.That(t, meta.MimeType, test.ShouldResemble, utils.MimeTypeJPEG)
}

func TestImages(t *testing.T) {
	logger := logging.NewTestLogger(t)

	namedImages := []camera.NamedImage{
		{Image: a, SourceName: ""},
		{Image: b, SourceName: ""},
		{Image: c, SourceName: ""},
	}

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
			ImagesFunc: func(ctx context.Context, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				return namedImages, resource.ResponseMetadata{CapturedAt: timestamp}, nil
			},
		},
		acceptedClassifications: map[string]map[string]float64{"": {"a": .8}},
		acceptedObjects:         map[string]map[string]float64{"": {"b": .8}},
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	res, meta, err := fc.Images(ctx, nil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldNotBeNil)

	test.That(t, len(res), test.ShouldEqual, 3)
	test.That(t, res, test.ShouldResemble, namedImages)

	test.That(t, meta, test.ShouldNotBeNil)
	test.That(t, meta.CapturedAt, test.ShouldResemble, timestamp)
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
			ImagesFunc: func(ctx context.Context, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				return []camera.NamedImage{
					{Image: a, SourceName: ""},
					{Image: b, SourceName: ""},
					{Image: c, SourceName: ""},
				}, resource.ResponseMetadata{}, nil
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
	imagesCam.ImagesFunc = func(ctx context.Context, extra map[string]interface{}) (
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
	// Verify ring buffer contains only the last 4 images (2, 3, 4, 5)
	test.That(t, fc.buf.GetRingBufferLength(), test.ShouldEqual, 4)

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
	// Verify ring buffer contains only the last 4 images (7, 8, 9, 10)
	test.That(t, fc.buf.GetRingBufferLength(), test.ShouldEqual, 4)

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
	imagesCam.ImagesFunc = func(ctx context.Context, extra map[string]interface{}) (
		[]camera.NamedImage, resource.ResponseMetadata, error) {
		timeCount++
		imageTime := baseTime.Add(time.Duration(timeCount) * time.Second)
		return []camera.NamedImage{
				{Image: image.NewRGBA(image.Rect(0, 0, 10, 10)), SourceName: fmt.Sprintf("img_%d", timeCount)},
			}, resource.ResponseMetadata{
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
	images1, _, err1 := fc.Images(ctx, map[string]interface{}{data.FromDMString: true})
	test.That(t, err1, test.ShouldEqual, data.ErrNoCaptureToStore) // Should fail - no trigger
	test.That(t, images1, test.ShouldBeNil)
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 0)
	// Ring buffer should have 4 images (ticks 1,2,3,4 background captures)
	test.That(t, fc.buf.GetRingBufferLength(), test.ShouldEqual, 4)
	expectedNames1 := []string{"img_1", "img_2", "img_3", "img_4"}
	for i, data := range fc.buf.GetRingBufferSlice() {
		img := data.Imgs[0]
		test.That(t, img.SourceName, test.ShouldEqual, expectedNames1[i])
	}

	// Ticks 5-8: Background captures
	for i := 5; i <= 8; i++ {
		fc.captureImageInBackground(ctx)
	}
	images2, _, err2 := fc.Images(ctx, map[string]interface{}{data.FromDMString: true})
	test.That(t, err2, test.ShouldEqual, data.ErrNoCaptureToStore) // Should fail - no trigger
	test.That(t, images2, test.ShouldBeNil)
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 0)
	// Ring buffer should have 5 images max (ticks 4,5,6,7,8)
	test.That(t, fc.buf.GetRingBufferLength(), test.ShouldEqual, 5)
	
	t.Logf("RingBuffer after second Images() call contains:")
	for i, data := range fc.buf.GetRingBufferSlice() {
		img := data.Imgs[0]
		t.Logf("  [%d]: %s", i, img.SourceName)
	}
	
	// With 4 more background captures: should have last 5 images (max buffer size)
	expectedNames2 := []string{"img_4", "img_5", "img_6", "img_7", "img_8"}
	for i, data := range fc.buf.GetRingBufferSlice() {
		img := data.Imgs[0]
		test.That(t, img.SourceName, test.ShouldEqual, expectedNames2[i])
	}

	// Change vision service to trigger condition
	visionSvc.ClassificationsFunc = func(ctx context.Context, img image.Image, n int, extra map[string]interface{}) (classification.Classifications, error) {
		return classification.Classifications{
			classification.NewClassification(0.9, "person"), // Above 0.8 threshold - should trigger
		}, nil
	}

	// Ticks 9-12: Background captures
	for i := 9; i <= 12; i++ {
		fc.captureImageInBackground(ctx)
	}
	// Ring buffer should still have 5 images max
	test.That(t, fc.buf.GetRingBufferLength(), test.ShouldEqual, 5)
	
	t.Logf("RingBuffer after ticks 9-12 contains:")
	for i, data := range fc.buf.GetRingBufferSlice() {
		img := data.Imgs[0]
		t.Logf("  [%d]: %s", i, img.SourceName)
	}
	
	// Based on previous pattern, we need to see what's actually in the ring buffer
	// Will adjust expected values after seeing the output

	images3, _, err3 := fc.Images(ctx, map[string]interface{}{data.FromDMString: true})
	t.Logf("Third Images() call returned %d images:", len(images3))
	for i, img := range images3 {
		t.Logf("  [%d]: %s", i, img.SourceName)
	}
	test.That(t, err3, test.ShouldBeNil) // Should succeed - trigger occurred
	// Trigger at tick 12: window is [9s, 14s] (12-3 to 12+2)
	// Available images in ring buffer: 8,9,10,11,12
	// Images 9,10,11,12 are within window [9,14], so expect 4 images
	test.That(t, len(images3), test.ShouldEqual, 4) // Initial batch: images 9,10,11,12
	// Verify exact image names in chronological order
	expectedNames3 := []string{"img_9", "img_10", "img_11", "img_12"}
	for i, img := range images3 {
		test.That(t, img.SourceName, test.ShouldEqual, expectedNames3[i])
	}
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 0) // Should be empty after PopAllToSend

	// Change vision service back to non-trigger condition
	visionSvc.ClassificationsFunc = func(ctx context.Context, img image.Image, n int, extra map[string]interface{}) (classification.Classifications, error) {
		return classification.Classifications{
			classification.NewClassification(0.5, "person"), // Below 0.8 threshold - no trigger
		}, nil
	}

	// Ticks 13-16: Background captures
	for i := 13; i <= 16; i++ {
		fc.captureImageInBackground(ctx)
	}
	images4, _, err4 := fc.Images(ctx, map[string]interface{}{data.FromDMString: true})
	t.Logf("Fourth Images() call returned %d images:", len(images4))
	for i, img := range images4 {
		t.Logf("  [%d]: %s", i, img.SourceName)
	}
	test.That(t, err4, test.ShouldBeNil) // Should succeed - still within window or has buffered images
	// Background worker added images 13,14 to ToSend (within window till 14s)
	// Images 15,16 should go to ring buffer (outside window)
	test.That(t, len(images4), test.ShouldEqual, 2) // Should get images 13,14

	expectedNames4 := []string{"img_13", "img_14"}
	for i, img := range images4 {
		test.That(t, img.SourceName, test.ShouldEqual, expectedNames4[i])
	}

	// Ticks 17-20: Background captures
	for i := 17; i <= 20; i++ {
		fc.captureImageInBackground(ctx)
	}
	images5, _, err5 := fc.Images(ctx, map[string]interface{}{data.FromDMString: true})
	test.That(t, err5, test.ShouldEqual, data.ErrNoCaptureToStore) // Should fail - outside window, no trigger
	test.That(t, images5, test.ShouldBeNil)
	test.That(t, fc.buf.GetToSendLength(), test.ShouldEqual, 0)
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
	imagesCam.ImagesFunc = func(ctx context.Context, extra map[string]interface{}) (
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
