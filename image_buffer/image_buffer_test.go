package imagebuffer

import (
	"testing"
	"time"

	"go.viam.com/rdk/logging"
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
	logger := logging.NewTestLogger(t)
	buf := NewImageBuffer(10, 1.0, 0, 0, logger, true, 0) // Enable debug for tests

	buf.ringBuffer = []CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
	}

	buf.MarkShouldSend(time.Now())

	// With the new implementation, we expect images within the window to be sent
	test.That(t, buf.GetToSendLength(), test.ShouldEqual, 2)
	toSendSlice := buf.GetToSendSlice()
	test.That(t, a, test.ShouldEqual, toSendSlice[0].Meta.CapturedAt)
	test.That(t, b, test.ShouldEqual, toSendSlice[1].Meta.CapturedAt)

	// Reset for second test
	buf.ringBuffer = []CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
	}
	buf.ClearToSend()

	buf.MarkShouldSend(time.Now())

	// Test that the ring buffer now only contains images that were NOT sent (c was outside window)
	test.That(t, buf.GetRingBufferLength(), test.ShouldEqual, 1)
	test.That(t, buf.GetToSendLength(), test.ShouldEqual, 2)
	toSendSlice = buf.GetToSendSlice()
	test.That(t, b, test.ShouldEqual, toSendSlice[0].Meta.CapturedAt)
	test.That(t, a, test.ShouldEqual, toSendSlice[1].Meta.CapturedAt)

}

func TestWindowBoundaries(t *testing.T) {

	// Initialize the image buffer
	logger := logging.NewTestLogger(t)
	buf := NewImageBuffer(0, 1.0, 5, 10, logger, true, 0) // Enable debug for tests

	buf.ringBuffer = []CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
	}

	buf.MarkShouldSend(time.Now())

	// With the new implementation, we expect images within the window to be sent
	test.That(t, buf.GetToSendLength(), test.ShouldEqual, 2)
	toSendSlice := buf.GetToSendSlice()
	test.That(t, a, test.ShouldEqual, toSendSlice[0].Meta.CapturedAt)
	test.That(t, b, test.ShouldEqual, toSendSlice[1].Meta.CapturedAt)

	// Reset for second test
	buf.ringBuffer = []CachedData{
		{Meta: resource.ResponseMetadata{CapturedAt: c}},
		{Meta: resource.ResponseMetadata{CapturedAt: b}},
		{Meta: resource.ResponseMetadata{CapturedAt: a}},
	}
	buf.ClearToSend()

	buf.MarkShouldSend(time.Now())

	// Test that the ring buffer now only contains images that were NOT sent (c was outside window)
	test.That(t, buf.GetRingBufferLength(), test.ShouldEqual, 1)
	test.That(t, buf.GetToSendLength(), test.ShouldEqual, 2)
	toSendSlice = buf.GetToSendSlice()
	test.That(t, b, test.ShouldEqual, toSendSlice[0].Meta.CapturedAt)
	test.That(t, a, test.ShouldEqual, toSendSlice[1].Meta.CapturedAt)

}

func TestZeroWindowCapturesOnlyTriggerImage(t *testing.T) {
	logger := logging.NewTestLogger(t)
	// All window values zero: no surrounding images are buffered
	buf := NewImageBuffer(0, 1.0, 0, 0, logger, true, 0)

	triggerTime := time.Now()

	// Images captured before the trigger never make it into ToSend
	buf.StoreImages(nil, resource.ResponseMetadata{CapturedAt: triggerTime.Add(-1 * time.Second)}, triggerTime.Add(-1*time.Second))
	test.That(t, buf.GetToSendLength(), test.ShouldEqual, 0)

	buf.MarkShouldSend(triggerTime)

	// The capture window collapses to the trigger instant, so only the
	// triggering image itself is queued
	test.That(t, buf.IsWithinCaptureWindow(triggerTime), test.ShouldBeTrue)
	buf.StoreImages(nil, resource.ResponseMetadata{CapturedAt: triggerTime}, triggerTime)
	test.That(t, buf.GetToSendLength(), test.ShouldEqual, 1)

	// Images after the trigger are outside the window
	after := triggerTime.Add(1 * time.Second)
	test.That(t, buf.IsWithinCaptureWindow(after), test.ShouldBeFalse)
	buf.StoreImages(nil, resource.ResponseMetadata{CapturedAt: after}, after)
	test.That(t, buf.GetToSendLength(), test.ShouldEqual, 1)
}

func TestCooldownBlocksRetrigger(t *testing.T) {
	logger := logging.NewTestLogger(t)
	// cooldown=5s, window=2s (before and after)
	buf := NewImageBuffer(2, 1.0, 0, 0, logger, true, 5)

	triggerTime := time.Now()
	buf.MarkShouldSend(triggerTime)

	// captureTill = triggerTime + 2s, cooldownTill = captureTill + 5s = triggerTime + 7s
	captureTill := triggerTime.Add(2 * time.Second)
	cooldownTill := triggerTime.Add(7 * time.Second)

	// Within capture window - not in cooldown
	test.That(t, buf.IsInCooldown(triggerTime.Add(1*time.Second)), test.ShouldBeFalse)

	// Just after capture window ends - should be in cooldown
	test.That(t, buf.IsInCooldown(captureTill.Add(1*time.Second)), test.ShouldBeTrue)

	// Still in cooldown near the end
	test.That(t, buf.IsInCooldown(captureTill.Add(4*time.Second)), test.ShouldBeTrue)

	// At cooldownTill boundary - still in cooldown (inclusive)
	test.That(t, buf.IsInCooldown(cooldownTill), test.ShouldBeTrue)

	// Past cooldownTill - no longer in cooldown
	test.That(t, buf.IsInCooldown(cooldownTill.Add(1*time.Second)), test.ShouldBeFalse)
}

func TestCooldownZeroHasNoEffect(t *testing.T) {
	logger := logging.NewTestLogger(t)
	// cooldown=0 means no cooldown
	buf := NewImageBuffer(2, 1.0, 0, 0, logger, true, 0)

	triggerTime := time.Now()
	buf.MarkShouldSend(triggerTime)

	// captureTill = triggerTime + 2s, cooldownTill = captureTill + 0s = captureTill
	captureTill := triggerTime.Add(2 * time.Second)

	// Just after capture window - should NOT be in cooldown when cooldown=0
	test.That(t, buf.IsInCooldown(captureTill.Add(1*time.Millisecond)), test.ShouldBeFalse)
	test.That(t, buf.IsInCooldown(captureTill.Add(1*time.Second)), test.ShouldBeFalse)
}

func TestCooldownExtendsWithRetrigger(t *testing.T) {
	logger := logging.NewTestLogger(t)
	// cooldown=5s, window before=2s, after=2s
	buf := NewImageBuffer(2, 1.0, 0, 0, logger, true, 5)

	trigger1 := time.Now()
	buf.MarkShouldSend(trigger1)
	// captureTill = trigger1 + 2s, cooldownTill = trigger1 + 7s

	// Second trigger within the capture window extends it
	trigger2 := trigger1.Add(1 * time.Second) // within first capture window
	buf.MarkShouldSend(trigger2)
	// captureTill = trigger2 + 2s = trigger1 + 3s, cooldownTill = trigger1 + 3s + 5s = trigger1 + 8s

	newCooldownTill := trigger1.Add(8 * time.Second)

	// Should be in cooldown up to the extended cooldownTill
	test.That(t, buf.IsInCooldown(trigger1.Add(4*time.Second)), test.ShouldBeTrue) // past captureTill (3s), before cooldownTill (8s)
	test.That(t, buf.IsInCooldown(newCooldownTill), test.ShouldBeTrue)             // at boundary
	test.That(t, buf.IsInCooldown(newCooldownTill.Add(1*time.Second)), test.ShouldBeFalse)
}

// TestZeroTimeIsNotWithinCaptureWindow covers the crash where a source camera that left
// ResponseMetadata.CapturedAt unset made every frame land in the unbounded ToSend
// buffer: before any trigger, captureFrom/captureTill are also the zero time, so the
// inclusive boundary check reported "inside the window" for a window never opened.
func TestZeroTimeIsNotWithinCaptureWindow(t *testing.T) {
	logger := logging.NewTestLogger(t)
	buf := NewImageBuffer(20, 1.0, 0, 0, logger, true, 0)

	var zero time.Time
	test.That(t, buf.IsWithinCaptureWindow(zero), test.ShouldBeFalse)
	test.That(t, buf.IsWithinCaptureWindow(time.Now()), test.ShouldBeFalse)

	// Unstamped frames must go to the bounded ring buffer, not to ToSend.
	for i := 0; i < 500; i++ {
		buf.StoreImages(nil, resource.ResponseMetadata{}, zero)
	}
	test.That(t, buf.GetToSendLength(), test.ShouldEqual, 0)
	test.That(t, buf.GetRingBufferLength(), test.ShouldBeLessThanOrEqualTo, buf.maxImages)

	// An open window still must not swallow unstamped frames.
	now := time.Now()
	buf.MarkShouldSend(now)
	test.That(t, buf.IsWithinCaptureWindow(zero), test.ShouldBeFalse)
	test.That(t, buf.IsWithinCaptureWindow(now), test.ShouldBeTrue)
}

// TestToSendIsBounded covers the other half of the crash: ToSend had no cap, so a
// consumer slower than image_frequency grew it until the module ran out of memory.
func TestToSendIsBounded(t *testing.T) {
	logger := logging.NewTestLogger(t)
	buf := NewImageBuffer(20, 1.0, 0, 0, logger, false, 0)

	maxToSendImages := buf.GetMaxToSendImages()
	test.That(t, maxToSendImages, test.ShouldBeGreaterThan, buf.toSendMaxWarningThreshold)

	// Open a capture window wide enough that every frame below is inside it.
	start := time.Now()
	buf.MarkShouldSend(start)

	// Produce far more than the cap without ever consuming, as the crashed machine did.
	total := maxToSendImages * 3
	for i := 0; i < total; i++ {
		buf.SetCaptureTill(start.Add(time.Hour))
		buf.StoreImages(nil, resource.ResponseMetadata{CapturedAt: start.Add(time.Duration(i) * time.Millisecond)},
			start.Add(time.Duration(i)*time.Millisecond))
	}

	test.That(t, buf.GetToSendLength(), test.ShouldEqual, maxToSendImages)
	test.That(t, buf.GetToSendDropped(), test.ShouldEqual, total-maxToSendImages)

	// The retained images must be the newest ones; the oldest are what got shed.
	toSend := buf.GetToSendSlice()
	test.That(t, toSend[len(toSend)-1].Meta.CapturedAt,
		test.ShouldEqual, start.Add(time.Duration(total-1)*time.Millisecond))
	test.That(t, toSend[0].Meta.CapturedAt,
		test.ShouldEqual, start.Add(time.Duration(total-maxToSendImages)*time.Millisecond))
}

// TestMarkShouldSendRespectsToSendCap makes sure the ring-buffer drain path is capped
// too, not just the direct StoreImages path.
func TestMarkShouldSendRespectsToSendCap(t *testing.T) {
	logger := logging.NewTestLogger(t)
	buf := NewImageBuffer(20, 1.0, 0, 0, logger, false, 0)

	maxToSendImages := buf.GetMaxToSendImages()
	now := time.Now()

	// Stuff the ring buffer past the ToSend cap, all inside the window MarkShouldSend
	// will open, so a single trigger tries to promote all of them at once.
	ring := make([]CachedData, 0, maxToSendImages*2)
	for i := 0; i < maxToSendImages*2; i++ {
		ring = append(ring, CachedData{Meta: resource.ResponseMetadata{CapturedAt: now.Add(time.Duration(-i) * time.Millisecond)}})
	}
	buf.ringBuffer = ring

	buf.MarkShouldSend(now)

	test.That(t, buf.GetToSendLength(), test.ShouldEqual, maxToSendImages)
	test.That(t, buf.GetToSendDropped(), test.ShouldEqual, maxToSendImages)
}
