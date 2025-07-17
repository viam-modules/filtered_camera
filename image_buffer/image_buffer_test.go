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

func TestWindow(t *testing.T) {

	// Initialize the image buffer
	buf := NewImageBuffer(10, 1.0)

	buf.RingBuffer = []CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
	}

	buf.MarkShouldSend(time.Now())

	// With the new implementation, we expect images within the window to be sent
	test.That(t, len(buf.ToSend), test.ShouldEqual, 2)
	test.That(t, a, test.ShouldEqual, buf.ToSend[0].Meta.CapturedAt)
	test.That(t, b, test.ShouldEqual, buf.ToSend[1].Meta.CapturedAt)

	// Reset for second test
	buf.RingBuffer = []CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
	}
	buf.ToSend = []CachedData{}

	buf.MarkShouldSend(time.Now())

	// Test that the ring buffer still contains images (not cleared like old Buffer)
	test.That(t, len(buf.RingBuffer), test.ShouldEqual, 3)
	test.That(t, len(buf.ToSend), test.ShouldEqual, 2)
	test.That(t, b, test.ShouldEqual, buf.ToSend[0].Meta.CapturedAt)
	test.That(t, a, test.ShouldEqual, buf.ToSend[1].Meta.CapturedAt)

}
