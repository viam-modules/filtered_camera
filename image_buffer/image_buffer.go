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
	mu           sync.Mutex
	recentImages []CachedData  // Upload these if something interesting happens in the near future
	toSend       []CachedData  // Upload these no matter what
	captureTill  time.Time
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

	ib.captureTill = time.Now().Add(ib.windowDuration(windowSeconds))
	if windowSeconds == 0 && len(ib.recentImages) != 0 {
		// If windowSeconds is 0, keep the most recent image (the one that triggered this function
		// call, which we should send) and throw out everything else.
		ib.toSend = append(ib.toSend, ib.recentImages[len(ib.recentImages)-1])
	} else {
		ib.cleanBuffer(windowSeconds)
		ib.toSend = append(ib.toSend, ib.recentImages...)
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
