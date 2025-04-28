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
	"go.viam.com/rdk/services/generic"

	"go.viam.com/utils"

	"github.com/viam-modules/filtered_camera"
)

var (
	Model            = filtered_camera.Family.WithModel("conditional-camera")
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

type conditionalCamera struct {
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	name   resource.Name
	conf   *Config
	logger logging.Logger

	cam     camera.Camera
	filtSvc resource.Resource
	buf     imagebuffer.ImageBuffer
}

func (cc *conditionalCamera) Name() resource.Name {
	return cc.name
}

func (cc *conditionalCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, resource.ErrDoUnimplemented
}

func (cc *conditionalCamera) Image(ctx context.Context, mimeType string, extra map[string]interface{}) ([]byte, camera.ImageMetadata, error) {
	ni, _, err := cc.images(ctx, extra)
	if err != nil {
		return nil, camera.ImageMetadata{}, err
	}

	return filtered_camera.ImagesToImage(ctx, ni, mimeType)
}

func (cc *conditionalCamera) Images(ctx context.Context) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	return cc.images(ctx, nil)
}

func (cc *conditionalCamera) images(ctx context.Context, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {

	images, meta, err := cc.cam.Images(ctx)
	if err != nil {
		return images, meta, err
	}

	if !filtered_camera.IsFromDataMgmt(ctx, extra) {
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

	if len(cc.buf.ToSend) > 0 {
		x := cc.buf.ToSend[0]
		cc.buf.ToSend = cc.buf.ToSend[1:]
		return x.Imgs, x.Meta, nil
	}

	return nil, meta, data.ErrNoCaptureToStore
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
