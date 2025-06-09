package imagebuffer

import (
	"testing"
	"time"

	"go.viam.com/rdk/resource"

	"go.viam.com/test"
)

var (
	a = time.Now()
	b = time.Now().Add(-1 * time.Second)
	c = time.Now().Add(-1 * time.Minute)
)

func TestUnsortedWindow(t *testing.T) {
	buf := ImageBuffer{}
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
}

func TestSortedWindow(t *testing.T) {
	buf := ImageBuffer{}
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
