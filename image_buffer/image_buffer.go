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
	Mu             sync.Mutex
	RingBuffer     []CachedData
	ToSend         []CachedData
	CaptureTill    time.Time
	LastCached     CachedData
	WindowSeconds  int
	ImageFrequency float64
}

func NewImageBuffer(windowSeconds int, imageFrequency float64) *ImageBuffer {
	return &ImageBuffer{
		RingBuffer:     []CachedData{},
		ToSend:         []CachedData{},
		WindowSeconds:  windowSeconds,
		ImageFrequency: imageFrequency,
	}
}

func (ib *ImageBuffer) windowDuration() time.Duration {
	return time.Second * time.Duration(ib.WindowSeconds)
}

func (ib *ImageBuffer) MarkShouldSend(now time.Time) {
	ib.Mu.Lock()
	defer ib.Mu.Unlock()

	ib.CaptureTill = now.Add(ib.windowDuration())

	// Send images from the ring buffer and continue collecting for windowDuration
	triggerTime := now
	var imagesToSend []CachedData

	// Create a map of existing timestamps in ToSend for O(1) lookup
	existingTimes := make(map[int64]bool)
	for _, existing := range ib.ToSend {
		existingTimes[existing.Meta.CapturedAt.UnixNano()] = true
	}

	// Add images from the ring buffer that are within the window
	windowDuration := ib.windowDuration()
	for _, cached := range ib.RingBuffer {
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
	ib.ToSend = append(ib.ToSend, imagesToSend...)
}

func (ib *ImageBuffer) AddToRingBuffer(imgs []camera.NamedImage, meta resource.ResponseMetadata) {
	ib.Mu.Lock()
	defer ib.Mu.Unlock()

	ib.RingBuffer = append(ib.RingBuffer, CachedData{imgs, meta})

	// Calculate the maximum number of images to keep in the ring buffer
	// Keep images for 2 * windowSeconds (before and after trigger)
	maxImages := int(2 * float64(ib.WindowSeconds) * ib.ImageFrequency)

	// Remove oldest images if we exceed the max
	if len(ib.RingBuffer) > maxImages {
		ib.RingBuffer = ib.RingBuffer[len(ib.RingBuffer)-maxImages:]
	}
}

func (ib *ImageBuffer) CacheImages(images []camera.NamedImage) {
	ib.Mu.Lock()
	defer ib.Mu.Unlock()

	ib.LastCached = CachedData{
		Imgs: images,
		Meta: resource.ResponseMetadata{
			CapturedAt: time.Now(),
		},
	}
}
