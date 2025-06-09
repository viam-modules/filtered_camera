// The filtered camera uses a vision service (classifier or detector) to decide whether images are
// interesting enough to get through the filter, whereas the confitional camera sends a DoCommand
// to any generic resource, and uses that response to decide whether to let images through.
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

	if !filtered_camera.IsFromDataMgmt(ctx, extra) {
		return images, meta, nil
	} else {
		// TODO: make the mutex private and lock it in AddToBuffer
		cc.buf.Mu.Lock()
		cc.buf.AddToBuffer_inlock(images, meta, cc.conf.WindowSeconds)
		cc.buf.Mu.Unlock()
	}

	for range images {
		err := cc.markShouldSend(ctx)
		if err != nil {
			return nil, meta, err
		}
	}

	x := cc.buf.GetCachedData()
	if x == nil {
		return nil, meta, data.ErrNoCaptureToStore
	}
	return x.Imgs, x.Meta, nil
}

func (cc *conditionalCamera) markShouldSend(ctx context.Context) error {

	ans, err := cc.filtSvc.DoCommand(ctx, nil)
	if err != nil {
		return err
	}

	// TODO: Make this configurable with "result" as default
	if ans["result"].(bool) {
		if time.Now().Before(cc.buf.CaptureTill) {
			// send, but don't update captureTill
			return nil
		}
		cc.buf.MarkShouldSend(cc.conf.WindowSeconds)
		return nil
	}

	return nil
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
