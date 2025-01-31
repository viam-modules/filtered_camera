package filtered_camera

import (
	"context"
	"image"
	"testing"
	"time"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/testutils/inject"
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
		if img == c {
			return []objectdetection.Detection{objectdetection.NewDetection(image.Rect(1, 1, 1, 1), .1, "b")}, nil
		}
		if img == b {
			return []objectdetection.Detection{objectdetection.NewDetection(image.Rect(1, 1, 1, 1), .9, "b")}, nil
		}
		if img == f {
			return []objectdetection.Detection{objectdetection.NewDetection(image.Rect(1, 1, 1, 1), .9, "f")}, nil
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
		vis:    getDummyVisionService(),
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

	fc.conf.Classifications["*"] = .8
	fc.conf.Objects["*"] = .8

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
		vis:    getDummyVisionService(),
	}

	a := time.Now()
	b := time.Now().Add(-1 * time.Second)
	c := time.Now().Add(-1 * time.Minute)

	fc.buffer = []cachedData{
		{meta: resource.ResponseMetadata{CapturedAt: a}},
		{meta: resource.ResponseMetadata{CapturedAt: b}},
		{meta: resource.ResponseMetadata{CapturedAt: c}},
	}

	fc.markShouldSend()

	test.That(t, len(fc.buffer), test.ShouldEqual, 0)
	test.That(t, len(fc.toSend), test.ShouldEqual, 2)
	test.That(t, b, test.ShouldEqual, fc.toSend[0].meta.CapturedAt)
	test.That(t, a, test.ShouldEqual, fc.toSend[1].meta.CapturedAt)

	fc.buffer = []cachedData{
		{meta: resource.ResponseMetadata{CapturedAt: c}},
		{meta: resource.ResponseMetadata{CapturedAt: b}},
		{meta: resource.ResponseMetadata{CapturedAt: a}},
	}
	fc.toSend = []cachedData{}

	fc.markShouldSend()

	test.That(t, len(fc.buffer), test.ShouldEqual, 0)
	test.That(t, len(fc.toSend), test.ShouldEqual, 2)
	test.That(t, b, test.ShouldEqual, fc.toSend[0].meta.CapturedAt)
	test.That(t, a, test.ShouldEqual, fc.toSend[1].meta.CapturedAt)

}
