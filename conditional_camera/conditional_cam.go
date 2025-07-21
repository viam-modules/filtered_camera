// The filtered camera uses a vision service (classifier or detector) to decide whether images are
// interesting enough to get through the filter, whereas the conditional camera sends a DoCommand
// to any generic resource, and uses that response to decide whether to let images through.
package conditional_camera

import (
	"context"

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
	WindowSecondsBefore int `json:"window_seconds_before"`
	WindowSecondsAfter int `json:"window_seconds_after"`
}

func (cfg *Config) Validate(path string) ([]string, error) {
	if cfg.Camera == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "camera")
	}

	if cfg.FilterSvc == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "filter_service")
	}
	
	if cfg.WindowSeconds > 0 {
		if cfg.WindowSecondsAfter > 0 || cfg.WindowSecondsBefore > 0 {
			return nil, errors.Errorf("If window_seconds > 0, then window_seconds_before and window_seconds_after must not be")
		}
	} else {
		if cfg.WindowSecondsAfter <= 0 && cfg.WindowSecondsBefore <= 0 {
			return nil, errors.Errorf("If window_seconds is not set, either window_seconds_before or window_seconds_after must be")
		}
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

	if filtered_camera.IsFromDataMgmt(ctx, extra) {
		cc.buf.AddToBuffer(images, meta, cc.conf.WindowSeconds)
	} else {
		return images, meta, nil
	}

	for range images {
		shouldSend, err := cc.shouldSend(ctx)
		if err != nil {
			return nil, meta, err
		}
		if shouldSend {
			cc.buf.MarkShouldSend(cc.conf.WindowSeconds)
		}
	}

	x := cc.buf.GetCachedData()
	if x == nil {
		return nil, meta, data.ErrNoCaptureToStore
	}
	return x.Imgs, x.Meta, nil
}

func (cc *conditionalCamera) shouldSend(ctx context.Context) (bool, error) {
	ans, err := cc.filtSvc.DoCommand(ctx, nil)
	if err != nil {
		return false, err
	}
	return ans["result"].(bool), nil
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
