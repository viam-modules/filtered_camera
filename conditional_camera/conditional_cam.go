package conditional_camera

import (
	"context"

	"time"

	"github.com/pkg/errors"
	imagebuffer "github.com/viam-modules/filtered_camera/image_buffer"
	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/rimage"
	"go.viam.com/rdk/services/generic"

	"go.viam.com/utils"
)

var (
	Model            = resource.ModelNamespace("viam").WithFamily("camera").WithModel("conditional-camera")
	errUnimplemented = errors.New("unimplemented")
)

type Config struct {
	Camera        string `json:"camera"`
	FilterSvc     string `json:"filter_service"`
	WindowSeconds int    `json:"window_seconds"`
}

func (cfg *Config) Validate(path string) ([]string, error) {
	if cfg.Camera == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "camera")
	}

	if cfg.FilterSvc == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "filter_service")
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

			cc := &conditionalCamera{name: conf.ResourceName(), conf: newConf, logger: logger}

			cc.cam, err = camera.FromDependencies(deps, newConf.Camera)
			if err != nil {
				return nil, err
			}

			cc.filtSvc, err = resource.FromDependencies[resource.Resource](deps, generic.Named(newConf.FilterSvc))
			if err != nil {
				return nil, err
			}

			return cc, nil
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

	buf imagebuffer.ImageBuffer
}

func (cc *conditionalCamera) Name() resource.Name {
	return cc.name
}

func (cc *conditionalCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, resource.ErrDoUnimplemented
}

func (cc *conditionalCamera) Images(ctx context.Context) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	images, meta, err := cc.cam.Images(ctx)
	if err != nil {
		return images, meta, err
	}

	// Search for a known key-value pair in the context.
	extra, ok := ctx.Value(int(0)).(map[string]interface{})
	if !ok || extra[data.FromDMString] != true {
		return images, meta, nil
	}

	for range images {
		shouldSend, err := cc.shouldSend(ctx)
		if err != nil {
			return nil, meta, err
		}

		if shouldSend {
			return images, meta, nil
		}
	}

	cc.buf.Mu.Lock()
	defer cc.buf.Mu.Unlock()

	cc.buf.AddToBuffer_inlock(images, meta, cc.conf.WindowSeconds)
	cc.buf.SendFromBuffer()

	return nil, meta, data.ErrNoCaptureToStore
}

func (cc *conditionalCamera) Image(ctx context.Context, mimeType string, extra map[string]interface{}) ([]byte, camera.ImageMetadata, error) {

	bytes, meta, err := cc.cam.Image(ctx, mimeType, extra)
	if err != nil {
		return nil, meta, err
	}
	ex, ok := ctx.Value(int(0)).(map[string]interface{})
	if !ok || ex[data.FromDMString] != true {
		return bytes, meta, nil
	}

	img, err := rimage.DecodeImage(ctx, bytes, meta.MimeType)
	if err != nil {
		return bytes, meta, err
	}

	shouldSend, err := cc.shouldSend(ctx)
	if err != nil {
		return bytes, meta, err
	}
	if shouldSend {
		return bytes, meta, nil
	}

	cc.buf.Mu.Lock()
	defer cc.buf.Mu.Unlock()

	cc.buf.AddToBuffer_inlock([]camera.NamedImage{{Image: img, SourceName: ""}}, resource.ResponseMetadata{CapturedAt: time.Now()}, cc.conf.WindowSeconds)

	return nil, meta, data.ErrNoCaptureToStore

}

func (cc *conditionalCamera) Close(ctx context.Context) error {
	return cc.cam.Close(ctx)
}

func (cc *conditionalCamera) shouldSend(ctx context.Context) (bool, error) {

	ans, err := cc.filtSvc.DoCommand(ctx, nil)
	if err != nil {
		return false, err
	}

	// TODO: Make this configurable with "result" as default
	if ans["result"].(bool) {
		if time.Now().Before(cc.buf.CaptureTill) {
			// send, but don't update captureTill
			return true, nil
		}
		cc.buf.MarkShouldSend(cc.conf.WindowSeconds)
		return true, nil
	}

	return false, nil
}

func (cc *conditionalCamera) NextPointCloud(ctx context.Context) (pointcloud.PointCloud, error) {
	return nil, errUnimplemented
}

func (cc *conditionalCamera) Properties(ctx context.Context) (camera.Properties, error) {
	p, err := cc.cam.Properties(ctx)
	if err == nil {
		p.SupportsPCD = false
	}
	return p, err
}
