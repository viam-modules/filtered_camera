package imagebuffer

import (
	"image"
	"testing"
	"time"

	"go.viam.com/rdk/resource"

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

func TestWindow(t *testing.T) {
	buf := ImageBuffer{}

	a := time.Now()
	b := time.Now().Add(-1 * time.Second)
	c := time.Now().Add(-1 * time.Minute)

	buf.recentPast = []CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
	}

	buf.MarkShouldSend(10)

	test.That(t, len(buf.recentPast), test.ShouldEqual, 0)
	test.That(t, len(buf.toSend), test.ShouldEqual, 2)
	test.That(t, b, test.ShouldEqual, buf.toSend[0].Meta.CapturedAt)
	test.That(t, a, test.ShouldEqual, buf.toSend[1].Meta.CapturedAt)

	buf.recentPast = []CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
	}
	buf.toSend = []CachedData{}

	buf.MarkShouldSend(10)

	test.That(t, len(buf.recentPast), test.ShouldEqual, 0)
	test.That(t, len(buf.toSend), test.ShouldEqual, 2)
	test.That(t, b, test.ShouldEqual, buf.toSend[0].Meta.CapturedAt)
	test.That(t, a, test.ShouldEqual, buf.toSend[1].Meta.CapturedAt)

}
