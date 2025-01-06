package conditional_camera

import (
	"context"
	"fmt"
	"image"
	"sort"
	"sync"
	"time"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"


	"go.viam.com/rdk/gostream"
	"go.viam.com/utils"
)

var Model = resource.ModelNamespace("viam").WithFamily("camera").WithModel("conditional-camera")

type Config struct {
	Camera        string
	FilterSvc     string
	WindowSeconds int `json:"window_seconds"`

	Classifications map[string]float64
	Objects         map[string]float64
}


func (cfg *Config) Validate(path string) ([]string, error) {
	if cfg.Camera == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "camera")
	}

	if cfg.FilterSvc == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "filter_svc")
	}

	return []string{cfg.Camera, cfg.FilterSvc}, nil
}

func init() {
	resource.RegisterComponent(camera.API, Model, resource.Registration[camera.Camera, *Config]{
		Constructor: func(ctx context.Context, deps resource.Dependencies, conf resource.Config, logger logging.Logger) (camera.Camera, error) {
			newConf, err := resource.NativeConfig[*Config](conf)
			if err != nil {
				return nil, err
			}

			fc := &conditionalCamera{name: conf.ResourceName(), conf: newConf, logger: logger}

			fc.cam, err = camera.FromDependencies(deps, newConf.Camera)
			if err != nil {
				return nil, err
			}

			fc.filtSvc, err = resource.FromDependencies[resource.Resource](deps, generic.Named(newConf.FilterSvc))
			if err != nil {
				return nil, err
			}

			return fc, nil
		},
	})
}

type cachedData struct {
	imgs []camera.NamedImage
	meta resource.ResponseMetadata
}

type conditionalCamera struct {
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	name   resource.Name
	conf   *Config
	logger logging.Logger

	cam     camera.Camera
	filtSvc resource.Resource

	mu          sync.Mutex
	buffer      []cachedData
	toSend      []cachedData
	captureTill time.Time
}

func (fc *conditionalCamera) Name() resource.Name {
	return fc.name
}

func (fc *conditionalCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, resource.ErrDoUnimplemented
}

func (fc *conditionalCamera) Images(ctx context.Context) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	images, meta, err := fc.cam.Images(ctx)
	if err != nil {
		return images, meta, err
	}

	extra, ok := camera.FromContext(ctx)
	if !ok || extra[data.FromDMString] != true {
		return images, meta, nil
	}

	for _, img := range images {
		shouldSend, err := fc.shouldSend(ctx, img.Image)
		if err != nil {
			return nil, meta, err
		}

		if shouldSend {
			return images, meta, nil
		}
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.addToBuffer_inlock(images, meta)

	if len(fc.toSend) > 0 {
		x := fc.toSend[0]
		fc.toSend = fc.toSend[1:]
		return x.imgs, x.meta, nil
	}

	return nil, meta, data.ErrNoCaptureToStore
}

func (fc *conditionalCamera) Stream(ctx context.Context, errHandlers ...gostream.ErrorHandler) (gostream.VideoStream, error) {
	camStream, err := fc.cam.Stream(ctx, errHandlers...)
	if err != nil {
		return nil, err
	}

	return filterStream{camStream, fc}, nil
}

type filterStream struct {
	cameraStream gostream.VideoStream
	fc           *conditionalCamera
}

func (fs filterStream) Next(ctx context.Context) (image.Image, func(), error) {
	extra, ok := camera.FromContext(ctx)
	if !ok || extra[data.FromDMString] != true {
		// If not data management collector, return underlying stream contents without filtering.
		return fs.cameraStream.Next(ctx)
	}

	img, release, err := fs.cameraStream.Next(ctx)
	if err != nil {
		return nil, nil, err
	}

	should, err := fs.fc.shouldSend(ctx, img)
	if err != nil {
		return nil, nil, err
	}

	if should {
		return img, release, nil
	}

	fs.fc.mu.Lock()
	defer fs.fc.mu.Unlock()

	fs.fc.addToBuffer_inlock([]camera.NamedImage{{img, ""}}, resource.ResponseMetadata{CapturedAt: time.Now()})

	return nil, nil, data.ErrNoCaptureToStore
}

func (fc *conditionalCamera) addToBuffer_inlock(imgs []camera.NamedImage, meta resource.ResponseMetadata) {
	if fc.conf.WindowSeconds == 0 {
		return
	}

	fc.cleanBuffer_inlock()
	fc.buffer = append(fc.buffer, cachedData{imgs, meta})
}

func (fs filterStream) Close(ctx context.Context) error {
	return fs.cameraStream.Close(ctx)
}

func (fc conditionalCamera) windowDuration() time.Duration {
	return time.Second * time.Duration(fc.conf.WindowSeconds)
}

func (fc *conditionalCamera) cleanBuffer_inlock() {
	sort.Slice(fc.buffer, func(i, j int) bool {
		a := fc.buffer[i]
		b := fc.buffer[j]
		return a.meta.CapturedAt.Before(b.meta.CapturedAt)
	})

	early := time.Now().Add(-1 * fc.windowDuration())
	for len(fc.buffer) > 0 {
		if fc.buffer[0].meta.CapturedAt.After(early) {
			return
		}
		fc.buffer = fc.buffer[1:]
	}
}

func (fc *conditionalCamera) markShouldSend() {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.captureTill = time.Now().Add(fc.windowDuration())
	fc.cleanBuffer_inlock()

	for _, x := range fc.buffer {
		fc.toSend = append(fc.toSend, x)
	}

	fc.buffer = []cachedData{}
}

func (fc *conditionalCamera) shouldSend(ctx context.Context, img image.Image) (bool, error) {

	ans, err := fc.filtSvc.DoCommand(ctx, nil)
	if err != nil {
		return false, err
	}

	// TODO: Make this configurable with "result" as default
	if ans["result"].(bool) {
		if time.Now().Before(fc.captureTill) {
			// send, but don't update captureTill
			return true, nil
		}
		fc.markShouldSend()
		return true, nil
	}

	return false, nil
}

func (fc *conditionalCamera) NextPointCloud(ctx context.Context) (pointcloud.PointCloud, error) {
	return nil, fmt.Errorf("filteredCamera doesn't support pointclouds yes")
}

func (fc *conditionalCamera) Properties(ctx context.Context) (camera.Properties, error) {
	p, err := fc.cam.Properties(ctx)
	if err == nil {
		p.SupportsPCD = false
	}
	return p, err
}
