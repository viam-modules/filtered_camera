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
	"go.viam.com/rdk/spatialmath"
	"go.viam.com/utils"

	"github.com/viam-modules/filtered_camera"
)

var (
	Model            = filtered_camera.Family.WithModel("conditional-camera")
	errUnimplemented = errors.New("unimplemented")
)

type Config struct {
	Camera              string  `json:"camera"`
	FilterSvc           string  `json:"filter_service"`
	WindowSeconds       int     `json:"window_seconds"`
	ImageFrequency      float64 `json:"image_frequency"`
	WindowSecondsBefore int     `json:"window_seconds_before"`
	WindowSecondsAfter  int     `json:"window_seconds_after"`
	Debug               bool    `json:"debug"`
}

func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.Camera == "" {
		return nil, nil, utils.NewConfigValidationFieldRequiredError(path, "camera")
	}

	if cfg.FilterSvc == "" {
		return nil, nil, utils.NewConfigValidationFieldRequiredError(path, "filter_service")
	}

	if cfg.ImageFrequency < 0 {
		return nil, nil, utils.NewConfigValidationError(path, errors.New("image_frequency must be greater than 0"))
	}

	if cfg.WindowSeconds < 0 || cfg.WindowSecondsBefore < 0 || cfg.WindowSecondsAfter < 0 {
		return nil, nil, utils.NewConfigValidationError(path,
			errors.New("none of window_seconds, window_seconds_after, or window_seconds_before can be negative"))
	} else if cfg.WindowSeconds > 0 && (cfg.WindowSecondsBefore > 0 || cfg.WindowSecondsAfter > 0) {
			return nil, nil, utils.NewConfigValidationError(path,
				errors.New("if window_seconds is set, window_seconds_before and window_seconds_after must not be"))
	}

	return []string{cfg.Camera, cfg.FilterSvc}, nil, nil
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

			// Initialize the image buffer
			imageFreq := newConf.ImageFrequency
			if imageFreq == 0 {
				imageFreq = 1.0
			}
			cc.buf = imagebuffer.NewImageBuffer(newConf.WindowSeconds, imageFreq, newConf.WindowSecondsBefore, newConf.WindowSecondsAfter, logger, newConf.Debug)

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
	buf     *imagebuffer.ImageBuffer
}

func (cc *conditionalCamera) Name() resource.Name {
	return cc.name
}

func (cc *conditionalCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, resource.ErrDoUnimplemented
}

func (cc *conditionalCamera) Image(ctx context.Context, mimeType string, extra map[string]interface{}) ([]byte, camera.ImageMetadata, error) {
	ni, _, err := cc.images(ctx, extra, true) // true indicates single image mode
	if err != nil {
		return nil, camera.ImageMetadata{}, err
	}

	return filtered_camera.ImagesToImage(ctx, ni, mimeType)
}

func (cc *conditionalCamera) Images(ctx context.Context, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	return cc.images(ctx, extra, false) // false indicates multiple images mode
}

func (cc *conditionalCamera) getBufferedImages(singleImageMode bool) ([]camera.NamedImage, resource.ResponseMetadata, bool) {
	if singleImageMode {
		if x, ok := cc.buf.PopFirstToSend(); ok {
			return x.Imgs, x.Meta, true
		}
	} else {
		if allImages, batchMeta, ok := cc.buf.PopAllToSend(); ok {
			return allImages, batchMeta, true
		}
	}
	return nil, resource.ResponseMetadata{}, false
}

func (cc *conditionalCamera) images(ctx context.Context, extra map[string]interface{}, singleImageMode bool) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	images, meta, err := cc.cam.Images(ctx, nil)
	if err != nil {
		return images, meta, err
	}

	if !filtered_camera.IsFromDataMgmt(ctx, extra) {
		return images, meta, nil
	}

	// If we're still within an active capture window, skip filter checks
	if cc.buf.IsWithinCaptureWindow(meta.CapturedAt) {
		if bufferedImages, bufferedMeta, ok := cc.getBufferedImages(singleImageMode); ok {
			return bufferedImages, bufferedMeta, nil
		}
		// If no buffered images, return current image (we're in capture mode)
		return images, meta, nil
	}

	// We're outside capture window, add to ring buffer and run filter checks
	cc.buf.AddToRingBuffer(images, meta)

	for range images {
		shouldSend, err := cc.shouldSend(ctx)
		if err != nil {
			return nil, meta, err
		}
		if shouldSend {
			cc.buf.MarkShouldSend(meta.CapturedAt)
		}
	}

	// Try to get buffered images
	if bufferedImages, bufferedMeta, ok := cc.getBufferedImages(singleImageMode); ok {
		return bufferedImages, bufferedMeta, nil
	}
	return nil, meta, data.ErrNoCaptureToStore
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

func (cc *conditionalCamera) Geometries(ctx context.Context, extra map[string]interface{}) ([]spatialmath.Geometry, error) {
	return nil, errors.New("unimplemented")
}
