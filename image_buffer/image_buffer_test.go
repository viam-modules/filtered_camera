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
