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

// ImageBuffer is a buffer for images.
type ImageBuffer struct {
	Mu          sync.Mutex
	Buffer      []CachedData
	ToSend      []CachedData
	CaptureTill time.Time
}

func (ib *ImageBuffer) AddToBuffer_inlock(imgs []camera.NamedImage, meta resource.ResponseMetadata, windowSeconds int) {
	if windowSeconds == 0 {
		return
	}

	ib.CleanBuffer_inlock(windowSeconds)
	ib.Buffer = append(ib.Buffer, CachedData{imgs, meta})
}

func (ib *ImageBuffer) CleanBuffer_inlock(windowSeconds int) {
	sort.Slice(ib.Buffer, func(i, j int) bool {
		a := ib.Buffer[i]
		b := ib.Buffer[j]
		return a.Meta.CapturedAt.Before(b.Meta.CapturedAt)
	})

	windowDuration := time.Duration(windowSeconds) * time.Second
	early := time.Now().Add(-1 * windowDuration)
	for len(ib.Buffer) > 0 {
		if ib.Buffer[0].Meta.CapturedAt.After(early) {
			return
		}
		ib.Buffer = ib.Buffer[1:]
	}
}

func (ib *ImageBuffer) MarkShouldSend(windowSeconds int) {
	ib.Mu.Lock()
	defer ib.Mu.Unlock()

	windowDuration := time.Duration(windowSeconds) * time.Second
	ib.CaptureTill = time.Now().Add(windowDuration)
	ib.CleanBuffer_inlock(windowSeconds)

	ib.ToSend = append(ib.ToSend, ib.Buffer...)
	ib.Buffer = []CachedData{}
}

// Mutex should be locked before running this command
func (ib *ImageBuffer) SendFromBuffer() ([]camera.NamedImage, resource.ResponseMetadata, error) {
	if len(ib.ToSend) > 0 {
		x := ib.ToSend[0]
		ib.ToSend = ib.ToSend[1:]
		return x.Imgs, x.Meta, nil
	}

	return nil, resource.ResponseMetadata{}, nil

}
