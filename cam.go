package filtered_camera

import (
	"context"
	"fmt"
	"image"
	"time"

	imagebuffer "github.com/viam-modules/filtered_camera/image_buffer"
	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/rimage"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/vision/classification"
	"go.viam.com/rdk/vision/objectdetection"
	"go.viam.com/utils"
)

var Model = resource.ModelNamespace("viam").WithFamily("camera").WithModel("filtered-camera")

type Config struct {
	Camera        string
	Vision        string
	WindowSeconds int `json:"window_seconds"`

	Classifications map[string]float64
	Objects         map[string]float64
}

func (cfg *Config) keepClassifications(cs []classification.Classification) bool {
	for _, c := range cs {
		if cfg.keepClassification(c) {
			return true
		}
	}
	return false
}

func (cfg *Config) keepClassification(c classification.Classification) bool {
	min, has := cfg.Classifications[c.Label()]
	if has && c.Score() > min {
		return true
	}

	min, has = cfg.Classifications["*"]
	if has && c.Score() > min {
		return true
	}

	return false
}

func (cfg *Config) keepObjects(ds []objectdetection.Detection) bool {
	for _, d := range ds {
		if cfg.keepObject(d) {
			return true
		}
	}

	return false
}

func (cfg *Config) keepObject(d objectdetection.Detection) bool {
	min, has := cfg.Objects[d.Label()]
	if has && d.Score() > min {
		return true
	}

	min, has = cfg.Objects["*"]
	if has && d.Score() > min {
		return true
	}

	return false
}

func (cfg *Config) Validate(path string) ([]string, error) {
	if cfg.Camera == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "camera")
	}

	if cfg.Vision == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "vision")
	}

	return []string{cfg.Camera, cfg.Vision}, nil
}

func init() {
	resource.RegisterComponent(camera.API, Model, resource.Registration[camera.Camera, *Config]{
		Constructor: func(ctx context.Context, deps resource.Dependencies, conf resource.Config, logger logging.Logger) (camera.Camera, error) {
			newConf, err := resource.NativeConfig[*Config](conf)
			if err != nil {
				return nil, err
			}

			fc := &filteredCamera{name: conf.ResourceName(), conf: newConf, logger: logger}

			fc.cam, err = camera.FromDependencies(deps, newConf.Camera)
			if err != nil {
				return nil, err
			}

			fc.vis, err = vision.FromDependencies(deps, newConf.Vision)
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

type filteredCamera struct {
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	name   resource.Name
	conf   *Config
	logger logging.Logger

	cam camera.Camera
	vis vision.Service

	buf imagebuffer.ImageBuffer
}

func (fc *filteredCamera) Name() resource.Name {
	return fc.name
}

func (fc *filteredCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, resource.ErrDoUnimplemented
}

func (fc *filteredCamera) Images(ctx context.Context) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	images, meta, err := fc.cam.Images(ctx)
	if err != nil {
		return images, meta, err
	}
	// Search for a known key-value pair in the context.
	extra, ok := ctx.Value(int(0)).(map[string]interface{})
	if !ok || extra[data.FromDMString] != true {
		// If not data management collector, return underlying contents without filtering.
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

	fc.buf.Mu.Lock()
	defer fc.buf.Mu.Unlock()

	fc.buf.AddToBuffer_inlock(images, meta, fc.conf.WindowSeconds)
	fc.buf.SendFromBuffer()

	return nil, meta, data.ErrNoCaptureToStore
}

func (fc *filteredCamera) Image(ctx context.Context, mimeType string, extra map[string]interface{}) ([]byte, camera.ImageMetadata, error) {

	bytes, meta, err := fc.cam.Image(ctx, mimeType, extra)
	if err != nil {
		return nil, meta, err
	}
	// Search for a known key-value pair in the context.
	ex, ok := ctx.Value(int(0)).(map[string]interface{})
	if !ok || ex[data.FromDMString] != true {
		// If not data management collector, return underlying contents without filtering.
		return bytes, meta, nil
	}

	img, err := rimage.DecodeImage(ctx, bytes, meta.MimeType)
	if err != nil {
		return bytes, meta, err
	}

	shouldSend, err := fc.shouldSend(ctx, img)
	if err != nil {
		return bytes, meta, err
	}
	if shouldSend {
		return bytes, meta, nil
	}

	fc.buf.Mu.Lock()
	defer fc.buf.Mu.Unlock()

	fc.buf.AddToBuffer_inlock([]camera.NamedImage{{Image: img, SourceName: ""}}, resource.ResponseMetadata{CapturedAt: time.Now()}, fc.conf.WindowSeconds)

	return nil, meta, data.ErrNoCaptureToStore
}

func (fc *filteredCamera) Close(ctx context.Context) error {
	return fc.cam.Close(ctx)
}

func (fc *filteredCamera) shouldSend(ctx context.Context, img image.Image) (bool, error) {

	if len(fc.conf.Classifications) > 0 {
		res, err := fc.vis.Classifications(ctx, img, 100, nil)
		if err != nil {
			return false, err
		}

		if fc.conf.keepClassifications(res) {
			fc.logger.Infof("keeping image with classifications %v", res)
			fc.buf.MarkShouldSend(fc.conf.WindowSeconds)
			return true, nil
		}
	}

	if len(fc.conf.Objects) > 0 {
		res, err := fc.vis.Detections(ctx, img, nil)
		if err != nil {
			return false, err
		}

		if fc.conf.keepObjects(res) {
			fc.logger.Infof("keeping image with objects %v", res)
			fc.buf.MarkShouldSend(fc.conf.WindowSeconds)
			return true, nil
		}
	}

	if time.Now().Before(fc.buf.CaptureTill) {
		// send, but don't update captureTill
		return true, nil
	}

	return false, nil
}

func (fc *filteredCamera) NextPointCloud(ctx context.Context) (pointcloud.PointCloud, error) {
	return nil, fmt.Errorf("filteredCamera doesn't support pointclouds yes")
}

func (fc *filteredCamera) Properties(ctx context.Context) (camera.Properties, error) {
	p, err := fc.cam.Properties(ctx)
	if err == nil {
		p.SupportsPCD = false
	}
	return p, err
}
