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
	mu          sync.Mutex
	recentPast  []CachedData
	toSend      []CachedData
	captureTill time.Time
	lastCached  CachedData
}

func (ib *ImageBuffer) windowDuration(windowSeconds int) time.Duration {
	return time.Second * time.Duration(windowSeconds)
}

// Remove too-old images from the RecentPast, then add the current image to the appropriate buffer
func (ib *ImageBuffer) AddToBuffer(imgs []camera.NamedImage, meta resource.ResponseMetadata, windowSeconds int) {
	ib.mu.Lock()
	defer ib.mu.Unlock()

	if windowSeconds == 0 {
		return
	}

	ib.cleanBuffer(windowSeconds)
	if ib.captureTill.Before(time.Now()) {
		ib.recentPast = append(ib.recentPast, CachedData{imgs, meta})
	} else {
		ib.toSend = append(ib.toSend, CachedData{imgs, meta})
	}
}

// Remove too-old images from RecentPast
func (ib *ImageBuffer) cleanBuffer(windowSeconds int) {
	sort.Slice(ib.recentPast, func(i, j int) bool {
		a := ib.recentPast[i]
		b := ib.recentPast[j]
		return a.Meta.CapturedAt.Before(b.Meta.CapturedAt)
	})

	early := time.Now().Add(-1 * ib.windowDuration(windowSeconds))
	for len(ib.recentPast) > 0 {
		if ib.recentPast[0].Meta.CapturedAt.After(early) {
			return
		}
		ib.recentPast = ib.recentPast[1:]
	}
}

func (ib *ImageBuffer) MarkShouldSend(windowSeconds int) {
	fmt.Println("top of MarkShouldSend")
	ib.mu.Lock()
	defer ib.mu.Unlock()

	ib.captureTill = time.Now().Add(ib.windowDuration(windowSeconds))
	ib.cleanBuffer(windowSeconds)

	ib.toSend = append(ib.toSend, ib.recentPast...)

	ib.recentPast = []CachedData{}
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
