package filtered_camera

import (
	"context"
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
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		acceptedClassifications: map[string]map[string]float64{"": {"a": .8}},
		acceptedObjects:         map[string]map[string]float64{"": {"b": .8}},
	}

	res, err := fc.shouldSend(context.Background(), d)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	res, err = fc.shouldSend(context.Background(), c)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	res, err = fc.shouldSend(context.Background(), b)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	res, err = fc.shouldSend(context.Background(), a)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// test wildcard

	res, err = fc.shouldSend(context.Background(), e)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	res, err = fc.shouldSend(context.Background(), f)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	fc.acceptedClassifications[""] = map[string]float64{"*": .8}
	fc.acceptedObjects[""] = map[string]float64{"*": .8}

	res, err = fc.shouldSend(context.Background(), e)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	res, err = fc.shouldSend(context.Background(), f)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// test inhibit should not send with classifications
	fc.inhibitedClassifications = map[string]map[string]float64{"": {"a": .7}}
	fc.acceptedObjects = map[string]map[string]float64{}
	fc.inhibitors = []vision.Service{
		getDummyVisionService(),
	}
	res, err = fc.shouldSend(context.Background(), a)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// test inhibit should not send with objects
	fc.inhibitedClassifications = map[string]map[string]float64{}
	fc.inhibitedObjects = map[string]map[string]float64{"": {"b": .1}}
	res, err = fc.shouldSend(context.Background(), b)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)

	// test that using same detector for inhibit and accept works properly
	fc.inhibitedObjects = map[string]map[string]float64{"": {"b": .7}}
	fc.acceptedObjects = map[string]map[string]float64{"": {"f": .7}}

	res, err = fc.shouldSend(context.Background(), b)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)
	res, err = fc.shouldSend(context.Background(), f)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	// test accepted stats update properly and don't affect rejected stats
	fc.acceptedStats = imageStats{}
	fc.rejectedStats = imageStats{}
	fc.inhibitors = []vision.Service{}
	fc.acceptedClassifications = map[string]map[string]float64{"": {"a": .8}}
	res, err = fc.shouldSend(context.Background(), a)
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
	res, err = fc.shouldSend(context.Background(), b)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, false)
	test.That(t, fc.rejectedStats.total, test.ShouldEqual, 1)
	test.That(t, fc.rejectedStats.breakdown["b"], test.ShouldEqual, 1)
	test.That(t, fc.acceptedStats.total, test.ShouldEqual, 0)
	_, ok = fc.acceptedStats.breakdown["b"]
	test.That(t, ok, test.ShouldEqual, false)

	// test that image that does not match any classification or object is rejected
	fc.rejectedStats = imageStats{}
	res, err = fc.shouldSend(context.Background(), d)
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

	res, err := conf.Validate(".")
	test.That(t, res, test.ShouldBeNil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "\"camera\" is required")
	conf.Camera = "foo"
	res, err = conf.Validate(".")
	test.That(t, res, test.ShouldBeNil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "\"vision_services\" is required")

	conf.Vision = "foo"
	res, err = conf.Validate(".")
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
	res, err = conf.Validate(".")
	test.That(t, res, test.ShouldBeNil)
	test.That(t, err, test.ShouldNotBeNil)
	test.That(t, err.Error(), test.ShouldContainSubstring, "cannot specify both vision and vision_services")

	// when vision is empty and vision_services is set, it should not error
	// and return the camera and vision service names
	conf.Vision = ""
	res, err = conf.Validate(".")
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
	res, err = conf.Validate(".")
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
	res, err = conf.Validate(".")
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
	res, err = conf.Validate(".")
	test.That(t, res, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, []string{"foo", "baz", "foo", "bar"})
}

func TestImage(t *testing.T) {
	logger := logging.NewTestLogger(t)

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.ImageBuffer{},
		cam: &inject.Camera{
			ImagesFunc: func(ctx context.Context) ([]camera.NamedImage, resource.ResponseMetadata, error) {
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
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.ImageBuffer{},
		cam: &inject.Camera{
			ImagesFunc: func(ctx context.Context) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				return namedImages, resource.ResponseMetadata{CapturedAt: timestamp}, nil
			},
		},
		acceptedClassifications: map[string]map[string]float64{"": {"a": .8}},
		acceptedObjects:         map[string]map[string]float64{"": {"b": .8}},
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, data.FromDMContextKey{}, true)

	res, meta, err := fc.Images(ctx)
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
		},
		logger: logger,
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.ImageBuffer{},
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
		},
		otherVisionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf: imagebuffer.ImageBuffer{},
		cam: &inject.Camera{
			ImagesFunc: func(ctx context.Context) ([]camera.NamedImage, resource.ResponseMetadata, error) {
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
