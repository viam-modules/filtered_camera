package imagebuffer

import (
	"sort"
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
	Mu          sync.Mutex
	Buffer      []CachedData
	RingBuffer  []CachedData
	ToSend      []CachedData
	CaptureTill time.Time
	LastCached  CachedData
}

func (ib *ImageBuffer) windowDuration(windowSeconds int) time.Duration {
	return time.Second * time.Duration(windowSeconds)
}

// Remove too-old images from the recentImages, then add the current image to the appropriate buffer
func (ib *ImageBuffer) AddToBuffer(imgs []camera.NamedImage, meta resource.ResponseMetadata, windowSeconds int) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	ib.cleanBuffer(windowSeconds)
	if ib.captureTill.Before(time.Now()) {
		ib.recentImages = append(ib.recentImages, CachedData{imgs, meta})
	} else {
		ib.toSend = append(ib.toSend, CachedData{imgs, meta})
	}
}

// Remove too-old images from recentImages
func (ib *ImageBuffer) cleanBuffer(windowSeconds int) {
	sort.Slice(ib.recentImages, func(i, j int) bool {
		a := ib.recentImages[i]
		b := ib.recentImages[j]
		return a.Meta.CapturedAt.Before(b.Meta.CapturedAt)
	})

	early := time.Now().Add(-1 * ib.windowDuration(windowSeconds))
	for len(ib.recentImages) > 0 {
		if ib.recentImages[0].Meta.CapturedAt.After(early) {
			return
		}
		ib.recentImages = ib.recentImages[1:]
	}
}

func (ib *ImageBuffer) MarkShouldSend(windowSeconds int) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	ib.CaptureTill = time.Now().Add(ib.windowDuration(windowSeconds))
	
	// Send images from the ring buffer and continue collecting for windowSeconds
	triggerTime := time.Now()
	var imagesToSend []CachedData
	
	// Add images from the ring buffer that are within the window
	for _, cached := range ib.RingBuffer {
		timeDiff := triggerTime.Sub(cached.Meta.CapturedAt)
		if timeDiff >= 0 && timeDiff <= ib.windowDuration(windowSeconds) {
			imagesToSend = append(imagesToSend, cached)
		}
	}
	
	// Add the images to send
	ib.ToSend = append(ib.ToSend, imagesToSend...)
}

func (ib *ImageBuffer) AddToRingBuffer(imgs []camera.NamedImage, meta resource.ResponseMetadata, windowSeconds int, imageFrequency float64) {
	ib.Mu.Lock()
	defer ib.Mu.Unlock()

	ib.RingBuffer = append(ib.RingBuffer, CachedData{imgs, meta})

	// Calculate the maximum number of images to keep in the ring buffer
	// Keep images for 2 * windowSeconds (before and after trigger)
	maxImages := int(2 * float64(windowSeconds) * imageFrequency)
	
	// Remove oldest images if we exceed the max
	if len(ib.RingBuffer) > maxImages {
		ib.RingBuffer = ib.RingBuffer[len(ib.RingBuffer)-maxImages:]
	}
}

func (ib *ImageBuffer) GetPostTriggerImages(windowSeconds int) []CachedData {
	ib.Mu.Lock()
	defer ib.Mu.Unlock()

	var postTriggerImages []CachedData
	cutoffTime := time.Now().Add(-ib.windowDuration(windowSeconds))
	
	for _, cached := range ib.RingBuffer {
		if cached.Meta.CapturedAt.After(cutoffTime) {
			postTriggerImages = append(postTriggerImages, cached)
		}
	}
	
	return postTriggerImages
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
	ib.recentImages = []CachedData{}
}

// Returns the oldest CachedData we're supposed to send. Returns nil if the buffer is empty.
func (ib *ImageBuffer) GetCachedData() *CachedData {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	if len(ib.toSend) == 0 {
		return nil
	}
	return_value := ib.toSend[0]
	ib.toSend = ib.toSend[1:]
	return &return_value
}
