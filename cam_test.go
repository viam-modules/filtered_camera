package filtered_camera

import (
	"context"
	"image"
	"testing"
	"time"

	imagebuffer "github.com/viam-modules/filtered_camera/image_buffer"
	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/rimage"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/testutils/inject"
	"go.viam.com/rdk/utils"
	"go.viam.com/rdk/vision/classification"
	"go.viam.com/rdk/vision/objectdetection"

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
		visionServices: []vision.Service{
			getDummyVisionService(),
		},
		allClassifications: map[string]map[string]float64{"": {"a": .8}},
		allObjects:         map[string]map[string]float64{"": {"b": .8}},
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

	fc.allClassifications[""] = map[string]float64{"*": .8}
	fc.allObjects[""] = map[string]float64{"*": .8}

	res, err = fc.shouldSend(context.Background(), e)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

	res, err = fc.shouldSend(context.Background(), f)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldEqual, true)

}

func TestWindow(t *testing.T) {
	logger := logging.NewTestLogger(t)

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
		},
		logger: logger,
		visionServices: []vision.Service{
			getDummyVisionService(),
		},
	}

	a := time.Now()
	b := time.Now().Add(-1 * time.Second)
	c := time.Now().Add(-1 * time.Minute)

	fc.buf.Buffer = []imagebuffer.CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
	}

	fc.buf.MarkShouldSend(fc.conf.WindowSeconds)

	test.That(t, len(fc.buf.Buffer), test.ShouldEqual, 0)
	test.That(t, len(fc.buf.ToSend), test.ShouldEqual, 2)
	test.That(t, b, test.ShouldEqual, fc.buf.ToSend[0].Meta.CapturedAt)
	test.That(t, a, test.ShouldEqual, fc.buf.ToSend[1].Meta.CapturedAt)

	fc.buf.Buffer = []imagebuffer.CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
	}
	fc.buf.ToSend = []imagebuffer.CachedData{}

	fc.buf.MarkShouldSend(fc.conf.WindowSeconds)

	test.That(t, len(fc.buf.Buffer), test.ShouldEqual, 0)
	test.That(t, len(fc.buf.ToSend), test.ShouldEqual, 2)
	test.That(t, b, test.ShouldEqual, fc.buf.ToSend[0].Meta.CapturedAt)
	test.That(t, a, test.ShouldEqual, fc.buf.ToSend[1].Meta.CapturedAt)

}

func TestValidate(t *testing.T) {
	conf := &Config{
		Classifications: map[string]float64{"a": .8},
		Objects:         map[string]float64{"b": .8},
		WindowSeconds:   10,
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
			Objects: 	   	 map[string]float64{"a": .8},
		},
	}
	res, err = conf.Validate(".")
	test.That(t, res, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, []string{"foo", "foo"})

	// vision services can not have any classifications or objects
	conf.VisionServices = []VisionServiceConfig{
		{
			Vision: "foo",
		},
	}
	res, err = conf.Validate(".")
	test.That(t, res, test.ShouldNotBeNil)
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res, test.ShouldResemble, []string{"foo", "foo"})
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
		visionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf:    imagebuffer.ImageBuffer{},
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
		visionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf:    imagebuffer.ImageBuffer{},
		cam: &inject.Camera{
			ImagesFunc: func(ctx context.Context) ([]camera.NamedImage, resource.ResponseMetadata, error) {
				return namedImages, resource.ResponseMetadata{CapturedAt: timestamp}, nil
			},
		},
	}

	ctx := context.Background()

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
		SupportsPCD:    false,
		ImageType:    	camera.ImageType("color"),
		MimeTypes:  	[]string{utils.MimeTypeJPEG},
	}

	fc := &filteredCamera{
		conf: &Config{
			Classifications: map[string]float64{"a": .8},
			Objects:         map[string]float64{"b": .8},
			WindowSeconds:   10,
		},
		logger: logger,
		visionServices: []vision.Service{
			getDummyVisionService(),
		},
		buf:    imagebuffer.ImageBuffer{},
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
