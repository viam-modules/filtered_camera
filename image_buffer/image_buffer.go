package imagebuffer

import (
	"sync"
	"time"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

const (
	timestampFormat = "2006-01-02T15:04:05.000Z07:00"
	noDateString    = "no-date"
	// minToSendWarningThreshold is the smallest ToSend buffer size that can
	// trigger a lagging-consumption warning.
	minToSendWarningThreshold = 10
	// toSendMaxImagesFactor scales the ToSend hard cap off the expected buffer size.
	// It sits well above toSendMaxWarningThreshold so the warning always fires first.
	toSendMaxImagesFactor = 5
	// minToSendMaxImages is the smallest hard cap placed on the ToSend buffer.
	minToSendMaxImages = 100
)

type CachedData struct {
	Imgs []camera.NamedImage
	Meta resource.ResponseMetadata
}

type ImageBuffer struct {
	mu                  sync.Mutex
	ringBuffer          []CachedData
	toSend              []CachedData
	captureFrom         time.Time
	captureTill         time.Time
	cooldownTill        time.Time
	cooldownSecs        int
	windowSecondsBefore int
	windowSecondsAfter  int
	imageFrequency      float64
	maxImages           int
	logger              logging.Logger
	debug               bool
	// toSendMaxWarningThreshold is the threshold for warning about ToSend buffer size
	toSendMaxWarningThreshold int
	// maxToSendImages is the hard cap on the ToSend buffer. Unlike ringBuffer, ToSend is only
	// drained when a consumer asks for images, so without a cap a consumer that runs
	// slower than imageFrequency grows it until the module runs out of memory.
	maxToSendImages int
	// toSendAtCap tracks whether we are currently shedding images, so the drop is
	// reported on the way in rather than once per dropped image.
	toSendAtCap bool
	// toSendOverThreshold tracks whether the lagging-consumption warning has already
	// been reported for the current episode. A backed-up buffer stays backed up, so
	// warning per image buries everything else in the log.
	toSendOverThreshold bool
	// toSendDropped counts images discarded because the ToSend cap was reached.
	toSendDropped int
}

func NewImageBuffer(windowSeconds int, imageFrequency float64, windowSecondsBefore int, windowSecondsAfter int, logger logging.Logger, debug bool, cooldownSecs int) *ImageBuffer {
	// Calculate the maximum number of images to keep in the ring buffer
	// Keep images for 2 * windowSeconds (before and after trigger)
	var maxImages int
	if windowSeconds > 0 {
		maxImages = int(3 * float64(windowSeconds) * imageFrequency)
		windowSecondsBefore = windowSeconds
		windowSecondsAfter = windowSeconds
	} else {
		maxImages = int(3 * float64(windowSecondsBefore+windowSecondsAfter) * imageFrequency)
	}
	return &ImageBuffer{
		ringBuffer:          []CachedData{},
		toSend:              []CachedData{},
		windowSecondsBefore: windowSecondsBefore,
		windowSecondsAfter:  windowSecondsAfter,
		cooldownSecs:        cooldownSecs,
		imageFrequency:      imageFrequency,
		maxImages:           maxImages,
		logger:              logger,
		debug:               debug,
		// Set warning threshold to 2x expected buffer size to detect when consumption is lagging,
		// with a floor so zero-window configs don't warn on every trigger image
		toSendMaxWarningThreshold: max(maxImages*2, minToSendWarningThreshold),
		maxToSendImages:           max(maxImages*toSendMaxImagesFactor, minToSendMaxImages),
	}
}

// withinCaptureWindowLocked reports whether now falls inside the currently open capture
// window. Callers must hold ib.mu.
//
// captureFrom and captureTill are the zero time until MarkShouldSend opens the first
// window, and a camera that does not populate ResponseMetadata hands us a zero
// CapturedAt. Comparing those two with Equal reports "inside the window" for a window
// that was never opened, so both are rejected up front.
func (ib *ImageBuffer) withinCaptureWindowLocked(now time.Time) bool {
	if ib.captureTill.IsZero() || now.IsZero() {
		return false
	}
	return !now.Before(ib.captureFrom) && !now.After(ib.captureTill)
}

// manageImageBufferCapLocked warns about a lagging consumer and sheds the oldest images
// once ToSend passes its hard cap. Both reports fire once per episode rather than once
// per image: the buffer stays backed up for as long as the consumer is behind, so
// per-image logging is what buried the real failure in the crash log.
//
// Shedding the oldest images keeps the module alive; letting the buffer grow costs the
// whole process, and with it every image already buffered. Callers must hold ib.mu.
func (ib *ImageBuffer) manageImageBufferCapLocked() {
	toSendLen := len(ib.toSend)

	// Below the warning threshold is also below the hard cap, since
	// toSendMaxWarningThreshold is always <= maxToSendImages.
	if toSendLen <= ib.toSendMaxWarningThreshold {
		ib.toSendOverThreshold = false
		ib.toSendAtCap = false
		return
	}

	if !ib.toSendOverThreshold {
		ib.toSendOverThreshold = true
		ib.logger.Warnf("ToSend buffer size (%d) exceeds warning threshold (%d). Images may be filling buffer faster than they are being consumed. Consider changing attribute \"image_frequency\" to match data capture frequency or slower.",
			toSendLen, ib.toSendMaxWarningThreshold)
	}

	if toSendLen <= ib.maxToSendImages {
		ib.toSendAtCap = false
		return
	}

	dropped := toSendLen - ib.maxToSendImages
	// Copy into a fresh backing array rather than re-slicing, so the dropped images
	// (and the pixel data they hold) actually become collectable.
	ib.toSend = append([]CachedData{}, ib.toSend[dropped:]...)
	ib.toSendDropped += dropped

	if !ib.toSendAtCap {
		ib.toSendAtCap = true
		ib.logger.Errorf("ToSend buffer reached its hard limit of %d images; dropping the oldest images to stay within memory. "+
			"Images are being captured faster than they are consumed - lower attribute \"image_frequency\" or capture data more often.",
			ib.maxToSendImages)
	}
}

func (ib *ImageBuffer) MarkShouldSend(triggerTime time.Time) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	// Add images from the ring buffer that are within the window
	beforeTimeBoundary := time.Second * time.Duration(ib.windowSecondsBefore)
	afterTimeBoundary := time.Second * time.Duration(ib.windowSecondsAfter)

	newCaptureFrom := triggerTime.Add(-beforeTimeBoundary)
	newCaptureTill := triggerTime.Add(afterTimeBoundary)
	// If we are in the middle of capturing new images, we want to keep the left boundary, i.e. the old captureFrom's value
	if ib.captureTill.Before(triggerTime) {
		ib.captureFrom = newCaptureFrom
	}
	ib.captureTill = newCaptureTill
	ib.cooldownTill = newCaptureTill.Add(time.Duration(ib.cooldownSecs) * time.Second)

	// Send images from the ring buffer and continue collecting for windowDuration
	var imagesToSend []CachedData
	var remainingRingBuffer []CachedData

	// Create a map of existing timestamps in ToSend for O(1) lookup
	existingTimes := make(map[int64]bool)
	for _, existing := range ib.toSend {
		existingTimes[existing.Meta.CapturedAt.UnixNano()] = true
	}

	for _, cached := range ib.ringBuffer {
		// Include images within captureFrom and captureTill boundaries, inclusive. Thus we have the not symbol here.
		if !cached.Meta.CapturedAt.Before(ib.captureFrom) && !cached.Meta.CapturedAt.After(ib.captureTill) {
			// Check if this image is already in ToSend to avoid duplicates
			if !existingTimes[cached.Meta.CapturedAt.UnixNano()] {
				imagesToSend = append(imagesToSend, cached)
			}
			// if its a duplicate, then discard it
		} else {
			// Outside capture window, keep in ring buffer
			remainingRingBuffer = append(remainingRingBuffer, cached)
		}
	}

	// Update ring buffer to exclude images that were added to ToSend
	ib.ringBuffer = remainingRingBuffer

	// Add the images to send
	ib.toSend = append(ib.toSend, imagesToSend...)
	ib.manageImageBufferCapLocked()

	toSendLen := len(ib.toSend)
	if ib.debug {
		ib.logger.Infow("MarkShouldSend completed",
			"method", "MarkShouldSend",
			"triggerTime", triggerTime.Format(timestampFormat),
			"captureFrom", ib.captureFrom.Format(timestampFormat),
			"captureTill", ib.captureTill.Format(timestampFormat),
			"cooldownTill", ib.cooldownTill.Format(timestampFormat),
			"imagesAdded", len(imagesToSend),
			"toSendSize", toSendLen,
			"ringBufferSize", len(ib.ringBuffer))
	}
}

func (ib *ImageBuffer) AddToRingBuffer(imgs []camera.NamedImage, meta resource.ResponseMetadata) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	ib.ringBuffer = append(ib.ringBuffer, CachedData{imgs, meta})

	// Remove oldest images if we exceed the max
	if len(ib.ringBuffer) > ib.maxImages {
		ib.ringBuffer = ib.ringBuffer[len(ib.ringBuffer)-ib.maxImages:]
	}
}

// SetCaptureTill sets the captureTill time
// This method is only used for testing purposes in cam_test.go
func (ib *ImageBuffer) SetCaptureTill(t time.Time) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	ib.captureTill = t
}

// SetCooldownTill sets the cooldownTill time
// This method is only used for testing purposes
func (ib *ImageBuffer) SetCooldownTill(t time.Time) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	ib.cooldownTill = t
}

// GetToSendLength returns the length of the toSend slice
func (ib *ImageBuffer) GetToSendLength() int {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return len(ib.toSend)
}

// GetMaxToSendImages returns the hard cap on the toSend slice
func (ib *ImageBuffer) GetMaxToSendImages() int {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return ib.maxToSendImages
}

// GetToSendDropped returns how many images have been discarded because toSend was full
func (ib *ImageBuffer) GetToSendDropped() int {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return ib.toSendDropped
}

// TimestampImagesToNames converts images to have timestamp-based names in format "[timestamp]_[original_name]"
func TimestampImagesToNames(images []camera.NamedImage, meta resource.ResponseMetadata) []camera.NamedImage {
	result := make([]camera.NamedImage, len(images))
	for i, img := range images {
		result[i] = img // Copy the image

		// Use timestamp as prefix - use "no-date" if timestamp not available
		timestampStr := noDateString
		if !meta.CapturedAt.IsZero() {
			timestampStr = meta.CapturedAt.Format(timestampFormat)
		}

		// Format: [timestamp]_[original_name]
		result[i].SourceName = timestampStr + "_" + img.SourceName
	}
	return result
}

// PopAllToSend removes and returns all elements from toSend slice as multiple images
func (ib *ImageBuffer) PopAllToSend() ([]camera.NamedImage, resource.ResponseMetadata, bool) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	if len(ib.toSend) == 0 {
		if ib.debug {
			ib.logger.Infow("PopAllToSend buffer empty",
				"method", "PopAllToSend",
				"toSendSize", 0)
		}
		return nil, resource.ResponseMetadata{}, false
	}

	// Combine all images from the ToSend buffer with individual timestamps
	var allImages []camera.NamedImage
	var earliestMeta resource.ResponseMetadata

	for i, cached := range ib.toSend {
		// Apply timestamp to each image in this cached data
		timestampedImages := TimestampImagesToNames(cached.Imgs, cached.Meta)
		allImages = append(allImages, timestampedImages...)

		// Use the earliest timestamp as the metadata for the batch
		if i == 0 || cached.Meta.CapturedAt.Before(earliestMeta.CapturedAt) {
			earliestMeta = cached.Meta
		}
	}

	if ib.debug {
		consumed := len(ib.toSend)
		ib.logger.Infow("PopAllToSend consumed images",
			"method", "PopAllToSend",
			"batchesConsumed", consumed,
			"totalImagesConsumed", len(allImages))
	}
	// Clear the ToSend buffer
	ib.toSend = []CachedData{}

	return allImages, earliestMeta, true
}

// ClearToSend clears the toSend slice
// Only used for testing purposes
func (ib *ImageBuffer) ClearToSend() {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	ib.toSend = []CachedData{}
}

// GetRingBufferLength returns the length of the ringBuffer slice
// Only used for testing purposes
func (ib *ImageBuffer) GetRingBufferLength() int {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return len(ib.ringBuffer)
}

// GetRingBufferSlice returns a copy of the RingBuffer slice for testing
// Only used for testing purposes
func (ib *ImageBuffer) GetRingBufferSlice() []CachedData {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return append([]CachedData{}, ib.ringBuffer...)
}

// GetToSendSlice returns a copy of the toSend slice for testing
// Only used for testing purposes
func (ib *ImageBuffer) GetToSendSlice() []CachedData {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return append([]CachedData{}, ib.toSend...)
}

// IsInCooldown returns true if the given time is after the capture window has ended
// but before the cooldown period has expired. During cooldown, new triggers should be suppressed.
func (ib *ImageBuffer) IsInCooldown(now time.Time) bool {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	afterCaptureWindow := now.After(ib.captureTill)
	beforeCooldownEnd := now.Before(ib.cooldownTill) || now.Equal(ib.cooldownTill)
	inCooldown := afterCaptureWindow && beforeCooldownEnd

	if ib.debug {
		ib.logger.Infow("IsInCooldown check",
			"method", "IsInCooldown",
			"now", now.Format(timestampFormat),
			"captureTill", ib.captureTill.Format(timestampFormat),
			"cooldownTill", ib.cooldownTill.Format(timestampFormat),
			"inCooldown", inCooldown)
	}

	return inCooldown
}

// IsWithinCaptureWindow returns true if the given time is within the current capture window
func (ib *ImageBuffer) IsWithinCaptureWindow(now time.Time) bool {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	withinWindow := ib.withinCaptureWindowLocked(now)

	if ib.debug {
		ib.logger.Infow("IsWithinCaptureWindow check",
			"method", "IsWithinCaptureWindow",
			"now", now.Format(timestampFormat),
			"captureFrom", ib.captureFrom.Format(timestampFormat),
			"captureTill", ib.captureTill.Format(timestampFormat),
			"withinWindow", withinWindow)
	}

	return withinWindow
}

// StoreImages intelligently stores images either in ToSend buffer (if within CaptureTill time)
// or in the RingBuffer (if outside CaptureTill time)
func (ib *ImageBuffer) StoreImages(images []camera.NamedImage, meta resource.ResponseMetadata, now time.Time) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	// if we're within the CaptureTill trigger time still, directly add the images to ToSend buffer
	// else then store them in the ring buffer
	if ib.withinCaptureWindowLocked(now) {
		cd := CachedData{Imgs: images, Meta: meta}
		ib.toSend = append(ib.toSend, cd)
		ib.manageImageBufferCapLocked()
		toSendLen := len(ib.toSend)
		if ib.debug {
			ib.logger.Infow("StoreImages: stored image to ToSend buffer",
				"method", "StoreImages",
				"withinCaptureWindow", true,
				"toSendSize", toSendLen)
		}
	} else {
		// Add to ring buffer (reuse existing logic)
		ib.ringBuffer = append(ib.ringBuffer, CachedData{Imgs: images, Meta: meta})

		// Remove oldest images if we exceed the max
		if len(ib.ringBuffer) > ib.maxImages {
			ib.ringBuffer = ib.ringBuffer[len(ib.ringBuffer)-ib.maxImages:]
		}
		if ib.debug {
			ib.logger.Infow("StoreImages: stored image to RingBuffer",
				"method", "StoreImages",
				"withinCaptureWindow", false,
				"ringBufferSize", len(ib.ringBuffer))
		}
	}
}
