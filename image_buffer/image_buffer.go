package imagebuffer

import (
	"sync"
	"time"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
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
	lastCached          CachedData
	windowSecondsBefore int
	windowSecondsAfter  int
	imageFrequency      float64
	maxImages           int
	logger              logging.Logger
	debug               bool
	// toSendMaxWarningThreshold is the threshold for warning about ToSend buffer size
	toSendMaxWarningThreshold int
}

func NewImageBuffer(windowSeconds int, imageFrequency float64, windowSecondsBefore int, windowSecondsAfter int, logger logging.Logger, debug bool) *ImageBuffer {
	// Calculate the maximum number of images to keep in the ring buffer
	// Keep images for 2 * windowSeconds (before and after trigger)
	var maxImages int
	if windowSeconds > 0 {
		maxImages = int(2 * float64(windowSeconds) * imageFrequency)
		windowSecondsBefore = windowSeconds
		windowSecondsAfter = windowSeconds
	} else {
		maxImages = int(float64(windowSecondsBefore+windowSecondsAfter) * imageFrequency)
	}
	return &ImageBuffer{
		ringBuffer:          []CachedData{},
		toSend:              []CachedData{},
		windowSecondsBefore: windowSecondsBefore,
		windowSecondsAfter:  windowSecondsAfter,
		imageFrequency:      imageFrequency,
		maxImages:           maxImages,
		logger:              logger,
		debug:               debug,
		// Set warning threshold to 2x expected buffer size to detect when consumption is lagging
		toSendMaxWarningThreshold: maxImages * 2,
	}
}

func (ib *ImageBuffer) MarkShouldSend(now time.Time) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	// Add images from the ring buffer that are within the window
	beforeTimeBoundary := time.Second * time.Duration(ib.windowSecondsBefore)
	afterTimeBoundary := time.Second * time.Duration(ib.windowSecondsAfter)

	newCaptureFrom := now.Add(-beforeTimeBoundary)
	newCaptureTill := now.Add(afterTimeBoundary)
	// If we are in the middle of capturing new images, we want to keep the left boundary, i.e. the old captureFrom's value
	if ib.captureTill.Before(now) {
		ib.captureFrom = newCaptureFrom
	}
	ib.captureTill = newCaptureTill

	// Send images from the ring buffer and continue collecting for windowDuration
	var imagesToSend []CachedData

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
		}
	}

	// Add the images to send
	ib.toSend = append(ib.toSend, imagesToSend...)
	
	// Log ToSend buffer size (only if debug enabled)
	toSendLen := len(ib.toSend)
	if ib.debug {
		ib.logger.Infof("MarkShouldSend: triggerTime=%v, captureFrom=%v, captureTill=%v, added %d images to ToSend buffer, total ToSend size: %d, RingBuffer size: %d", 
			now, ib.captureFrom, ib.captureTill, len(imagesToSend), toSendLen, len(ib.ringBuffer))
	}
	
	// Warn if ToSend buffer is getting too large (always warn, regardless of debug setting)
	if toSendLen > ib.toSendMaxWarningThreshold {
		ib.logger.Warnf("ToSend buffer size (%d) exceeds warning threshold (%d). Images may be filling buffer faster than they are being consumed. Consider changing attribute \"image_frequency\" to match data capture frequency or slower.", 
			toSendLen, ib.toSendMaxWarningThreshold)
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

func (ib *ImageBuffer) CacheImages(images []camera.NamedImage) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	ib.lastCached = CachedData{
		Imgs: images,
		Meta: resource.ResponseMetadata{
			CapturedAt: time.Now(),
		},
	}
}

// SetCaptureTill sets the captureTill time
// This method is only used for testing purposes in cam_test.go
func (ib *ImageBuffer) SetCaptureTill(t time.Time) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	ib.captureTill = t
}

// GetToSendLength returns the length of the toSend slice
func (ib *ImageBuffer) GetToSendLength() int {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return len(ib.toSend)
}

// PopFirstToSend removes and returns the first element from toSend slice
func (ib *ImageBuffer) PopFirstToSend() (CachedData, bool) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	if len(ib.toSend) == 0 {
		if ib.debug {
			ib.logger.Infof("PopFirstToSend: ToSend buffer is empty")
		}
		return CachedData{}, false
	}
	x := ib.toSend[0]
	ib.toSend = ib.toSend[1:]
	if ib.debug {
		remainingLen := len(ib.toSend)
		ib.logger.Infof("PopFirstToSend: consumed 1 image from ToSend buffer, remaining ToSend size: %d", remainingLen)
	}
	return x, true
}

// PopAllToSend removes and returns all elements from toSend slice as multiple images
func (ib *ImageBuffer) PopAllToSend() ([]camera.NamedImage, resource.ResponseMetadata, bool) {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	if len(ib.toSend) == 0 {
		if ib.debug {
			ib.logger.Infof("PopAllToSend: ToSend buffer is empty")
		}
		return nil, resource.ResponseMetadata{}, false
	}

	// Combine all images from the ToSend buffer
	var allImages []camera.NamedImage
	var earliestMeta resource.ResponseMetadata

	for i, cached := range ib.toSend {
		allImages = append(allImages, cached.Imgs...)
		// Use the earliest timestamp as the metadata for the batch
		if i == 0 || cached.Meta.CapturedAt.Before(earliestMeta.CapturedAt) {
			earliestMeta = cached.Meta
		}
	}

	// Clear the ToSend buffer
	consumed := len(ib.toSend)
	ib.toSend = []CachedData{}
	
	if ib.debug {
		ib.logger.Infof("PopAllToSend: consumed %d image batches (%d total images) from ToSend buffer", consumed, len(allImages))
	}
	
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

// GetToSendSlice returns a copy of the toSend slice for testing
// Only used for testing purposes
func (ib *ImageBuffer) GetToSendSlice() []CachedData {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	return append([]CachedData{}, ib.toSend...)
}

// IsWithinCaptureWindow returns true if the given time is within the current capture window
func (ib *ImageBuffer) IsWithinCaptureWindow(now time.Time) bool {
	ib.mu.Lock()
	defer ib.mu.Unlock()
	withinWindow := (now.Before(ib.captureTill) && now.After(ib.captureFrom)) || now.Equal(ib.captureTill) || now.Equal(ib.captureFrom)
	
	if ib.debug {
		ib.logger.Infof("IsWithinCaptureWindow: now=%v, captureFrom=%v, captureTill=%v, withinWindow=%v", 
			now, ib.captureFrom, ib.captureTill, withinWindow)
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
	if (now.Before(ib.captureTill) && now.After(ib.captureFrom)) || now.Equal(ib.captureTill) || now.Equal(ib.captureFrom) {
		ib.toSend = append(ib.toSend, CachedData{Imgs: images, Meta: meta})
		toSendLen := len(ib.toSend)
		if ib.debug {
			ib.logger.Infof("StoreImages: stored 1 image directly to ToSend buffer (within capture window), ToSend size: %d", toSendLen)
		}
		
		// Warn if ToSend buffer is getting too large (always warn, regardless of debug setting)
		if toSendLen > ib.toSendMaxWarningThreshold {
			ib.logger.Warnf("ToSend buffer size (%d) exceeds warning threshold (%d). Images may be filling buffer faster than they are being consumed. Consider changing attribute \"image_frequency\" to match data capture frequency or slower.", 
				toSendLen, ib.toSendMaxWarningThreshold)
		}
	} else {
		// Add to ring buffer (reuse existing logic)
		ib.ringBuffer = append(ib.ringBuffer, CachedData{Imgs: images, Meta: meta})

		// Remove oldest images if we exceed the max
		if len(ib.ringBuffer) > ib.maxImages {
			ib.ringBuffer = ib.ringBuffer[len(ib.ringBuffer)-ib.maxImages:]
		}
		if ib.debug {
			ib.logger.Infof("StoreImages: stored 1 image to RingBuffer (outside capture window), RingBuffer size: %d", len(ib.ringBuffer))
		}
	}
}
