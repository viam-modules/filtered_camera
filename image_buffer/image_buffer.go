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
	RecentPast  []CachedData
	ToSend      []CachedData
	CaptureTill time.Time
	LastCached  CachedData
}

func (ib *ImageBuffer) windowDuration(windowSeconds int) time.Duration {
	return time.Second * time.Duration(windowSeconds)
}

// Remove too-old images from the RecentPast, then add the current image to the appropriate buffer
func (ib *ImageBuffer) AddToBuffer_inlock(imgs []camera.NamedImage, meta resource.ResponseMetadata, windowSeconds int) {
	if windowSeconds == 0 {
		return
	}

	ib.CleanBuffer_inlock(windowSeconds)
	if ib.CaptureTill.Before(time.Now()) {
		ib.RecentPast = append(ib.RecentPast, CachedData{imgs, meta})
	} else {
		ib.ToSend = append(ib.ToSend, CachedData{imgs, meta})
	}
}

// Remove too-old images from RecentPast
func (ib *ImageBuffer) CleanBuffer_inlock(windowSeconds int) {
	sort.Slice(ib.RecentPast, func(i, j int) bool {
		a := ib.RecentPast[i]
		b := ib.RecentPast[j]
		return a.Meta.CapturedAt.Before(b.Meta.CapturedAt)
	})

	early := time.Now().Add(-1 * ib.windowDuration(windowSeconds))
	for len(ib.RecentPast) > 0 {
		if ib.RecentPast[0].Meta.CapturedAt.After(early) {
			return
		}
		ib.RecentPast = ib.RecentPast[1:]
	}
}

func (ib *ImageBuffer) MarkShouldSend(windowSeconds int) {
	ib.Mu.Lock()
	defer ib.Mu.Unlock()

	ib.CaptureTill = time.Now().Add(ib.windowDuration(windowSeconds))
	ib.CleanBuffer_inlock(windowSeconds)

	ib.ToSend = append(ib.ToSend, ib.RecentPast...)

	ib.RecentPast = []CachedData{}
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

// Returns the oldest CachedData we're supposed to send. Returns nil if the buffer is empty.
func (ib *ImageBuffer) GetCachedData() *CachedData {
	ib.Mu.Lock()
	defer ib.Mu.Unlock()

	if len(ib.ToSend) == 0 {
		return nil
	}
	return_value := ib.ToSend[0]
	ib.ToSend = ib.ToSend[1:]
	return &return_value
}
