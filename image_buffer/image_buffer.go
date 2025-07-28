package imagebuffer

import (
	"sync"
	"time"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/resource"
)

type CachedData struct {
	Imgs []camera.NamedImage
	Meta resource.ResponseMetadata
}

type ImageBuffer struct {
	mu             sync.Mutex
	ringBuffer     []CachedData
	toSend         []CachedData
	captureTill    time.Time
	lastCached     CachedData
	windowSeconds  int
	imageFrequency float64
	maxImages      int
}

func NewImageBuffer(windowSeconds int, imageFrequency float64) *ImageBuffer {
	// Calculate the maximum number of images to keep in the ring buffer
	// Keep images for 2 * windowSeconds (before and after trigger)
	maxImages := int(2 * float64(windowSeconds) * imageFrequency)
	return &ImageBuffer{
		ringBuffer:     []CachedData{},
		toSend:         []CachedData{},
		windowSeconds:  windowSeconds,
		imageFrequency: imageFrequency,
		maxImages:      maxImages,
	}
}

func (ib *ImageBuffer) windowDuration() time.Duration {
	return time.Second * time.Duration(ib.windowSeconds)
}

func (ib *ImageBuffer) MarkShouldSend(now time.Time) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	ib.captureTill = now.Add(ib.windowDuration())

	// Send images from the ring buffer and continue collecting for windowDuration
	triggerTime := now
	var imagesToSend []CachedData

	// Create a map of existing timestamps in ToSend for O(1) lookup
	existingTimes := make(map[int64]bool)
	for _, existing := range ib.toSend {
		existingTimes[existing.Meta.CapturedAt.UnixNano()] = true
	}

	// Add images from the ring buffer that are within the window
	windowDuration := ib.windowDuration()
	for _, cached := range ib.ringBuffer {
		timeDiff := triggerTime.Sub(cached.Meta.CapturedAt)
		// Include images within windowSeconds before and after trigger
		if timeDiff >= -windowDuration && timeDiff <= windowDuration {
			// Check if this image is already in ToSend to avoid duplicates
			if !existingTimes[cached.Meta.CapturedAt.UnixNano()] {
				imagesToSend = append(imagesToSend, cached)
			}
		}
	}

	// Add the images to send
	ib.toSend = append(ib.toSend, imagesToSend...)
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
		return CachedData{}, false
	}
	x := ib.toSend[0]
	ib.toSend = ib.toSend[1:]
	return x, true
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

// StoreImages intelligently stores images either in ToSend buffer (if within CaptureTill time)
// or in the RingBuffer (if outside CaptureTill time)
func (ib *ImageBuffer) StoreImages(images []camera.NamedImage, meta resource.ResponseMetadata, now time.Time) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	// if we're within the CaptureTill trigger time still, directly add the images to ToSend buffer
	// else then store them in the ring buffer
	if now.Before(ib.captureTill) || now.Equal(ib.captureTill) {
		ib.toSend = append(ib.toSend, CachedData{Imgs: images, Meta: meta})
	} else {
		// Add to ring buffer (reuse existing logic)
		ib.ringBuffer = append(ib.ringBuffer, CachedData{Imgs: images, Meta: meta})

		// Remove oldest images if we exceed the max
		if len(ib.ringBuffer) > ib.maxImages {
			ib.ringBuffer = ib.ringBuffer[len(ib.ringBuffer)-ib.maxImages:]
		}
	}
}
