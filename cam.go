package filtered_camera

import (
	"context"
	"errors"
	"fmt"
	"image"
	"time"

	imagebuffer "github.com/viam-modules/filtered_camera/image_buffer"
	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/vision/classification"
	"go.viam.com/rdk/vision/objectdetection"
	"go.viam.com/utils"
)

var Model = Family.WithModel("filtered-camera")

type Config struct {
	Camera string
	// Deprecated: use VisionServices instead
	Vision         string
	VisionServices []VisionServiceConfig `json:"vision_services,omitempty"`
	WindowSeconds  int                   `json:"window_seconds"`

	Classifications map[string]float64
	Objects         map[string]float64
}

type VisionServiceConfig struct {
	Vision          string             `json:"vision"`
	Objects         map[string]float64 `json:"objects,omitempty"`
	Classifications map[string]float64 `json:"classifications,omitempty"`
	Inhibit         bool               `json:"inhibit"`
}

// Validate ensures all parts of the config are valid.
func (config *VisionServiceConfig) Validate(path string) error {
	if config.Vision == "" {
		return resource.NewConfigValidationFieldRequiredError(path, "vision")
	}

	return nil
}

func (cfg *Config) Validate(path string) ([]string, error) {
	if cfg.Camera == "" {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "camera")
	}

	if cfg.Vision == "" && cfg.VisionServices == nil {
		return nil, utils.NewConfigValidationFieldRequiredError(path, "vision_services")
	} else if cfg.Vision != "" && cfg.VisionServices != nil {
		return nil, utils.NewConfigValidationError(path, errors.New("cannot specify both vision and vision_services"))
	}

	deps := []string{cfg.Camera}
	inhibitors := []string{}
	otherVisionServices := []string{}

	if cfg.Vision != "" {
		logger := logging.NewBlankLogger("deprecated")
		logger.Warnf("vision is deprecated, please use vision_services instead")
		deps = append(deps, cfg.Vision)
	} else {
		for idx, vs := range cfg.VisionServices {
			if err := vs.Validate(fmt.Sprintf("%s.%s.%d", path, "vision-service", idx)); err != nil {
				return nil, err
			}
			if vs.Inhibit {
				inhibitors = append(inhibitors, vs.Vision)
			} else {
				otherVisionServices = append(otherVisionServices, vs.Vision)
			}
		}
	}

	deps = append(deps, inhibitors...)
	deps = append(deps, otherVisionServices...)

	return deps, nil
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
			if newConf.Vision != "" {
				fc.otherVisionServices = make([]vision.Service, 1)
				fc.otherVisionServices[0], err = vision.FromDependencies(deps, newConf.Vision)
				if err != nil {
					return nil, err
				}

				if newConf.Classifications != nil {
					fc.allClassifications = make(map[string]map[string]float64)
					fc.allClassifications[newConf.Vision] = newConf.Classifications
				}
				if newConf.Objects != nil {
					fc.allObjects = make(map[string]map[string]float64)
					fc.allObjects[newConf.Vision] = newConf.Objects
				}
			} else {
				fc.inhibitors = []vision.Service{}
				fc.otherVisionServices = []vision.Service{}
				for _, vs := range newConf.VisionServices {
					visionService, err := vision.FromDependencies(deps, vs.Vision)
					if err != nil {
						return nil, err
					}

					if vs.Inhibit {
						fc.inhibitors = append(fc.inhibitors, visionService)
					} else {
						fc.otherVisionServices = append(fc.otherVisionServices, visionService)
					}

					if vs.Classifications != nil {
						if fc.allClassifications == nil {
							fc.allClassifications = make(map[string]map[string]float64)
						}
						fc.allClassifications[vs.Vision] = vs.Classifications
					}
					if vs.Objects != nil {
						if fc.allObjects == nil {
							fc.allObjects = make(map[string]map[string]float64)
						}
						fc.allObjects[vs.Vision] = vs.Objects
					}
				}
			}

			return fc, nil
		},
	})
}

type filteredCamera struct {
	resource.AlwaysRebuild
	resource.TriviallyCloseable

	name   resource.Name
	conf   *Config
	logger logging.Logger

	cam                 camera.Camera
	buf                 imagebuffer.ImageBuffer
	inhibitors          []vision.Service
	otherVisionServices []vision.Service
	allClassifications  map[string]map[string]float64
	allObjects          map[string]map[string]float64
}

func (fc *filteredCamera) anyClassificationsMatch(visionService string, cs []classification.Classification) bool {
	for _, c := range cs {
		if fc.classificationMatches(visionService, c) {
			return true
		}
	}
	return false
}

func (fc *filteredCamera) classificationMatches(visionService string, c classification.Classification) bool {
	min, has := fc.allClassifications[visionService][c.Label()]
	if has && c.Score() > min {
		return true
	}

	min, has = fc.allClassifications[visionService]["*"]
	if has && c.Score() > min {
		return true
	}

	return false
}

func (fc *filteredCamera) anyDetectionsMatch(visionService string, ds []objectdetection.Detection) bool {
	for _, d := range ds {
		if fc.detectionMatches(visionService, d) {
			return true
		}
	}

	return false
}

func (fc *filteredCamera) detectionMatches(visionService string, d objectdetection.Detection) bool {
	min, has := fc.allObjects[visionService][d.Label()]
	if has && d.Score() > min {
		return true
	}

	min, has = fc.allObjects[visionService]["*"]
	if has && d.Score() > min {
		return true
	}

	return false
}

func (fc *filteredCamera) Name() resource.Name {
	return fc.name
}

func (fc *filteredCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return nil, resource.ErrDoUnimplemented
}

func (fc *filteredCamera) Image(ctx context.Context, mimeType string, extra map[string]interface{}) ([]byte, camera.ImageMetadata, error) {
	ni, _, err := fc.images(ctx, extra)
	if err != nil {
		return nil, camera.ImageMetadata{}, err
	}

	return ImagesToImage(ctx, ni, mimeType)
}

func (fc *filteredCamera) Images(ctx context.Context) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	return fc.images(ctx, nil)
}

func (fc *filteredCamera) images(ctx context.Context, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {

	images, meta, err := fc.cam.Images(ctx)
	if err != nil {
		return images, meta, err
	}

	if !IsFromDataMgmt(ctx, extra) {
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

	if len(fc.buf.ToSend) > 0 {
		x := fc.buf.ToSend[0]
		fc.buf.ToSend = fc.buf.ToSend[1:]
		return x.Imgs, x.Meta, nil
	}

	return nil, meta, data.ErrNoCaptureToStore
}

func (fc *filteredCamera) shouldSend(ctx context.Context, img image.Image) (bool, error) {
	// inhibitors are first priority
	for _, vs := range fc.inhibitors {
		if len(fc.allClassifications[vs.Name().Name]) > 0 {
			res, err := vs.Classifications(ctx, img, 100, nil)
			if err != nil {
				return false, err
			}

			match := fc.anyClassificationsMatch(vs.Name().Name, res)
			if match {
				fc.logger.Debugf("rejecting image with classifications %v", res)
				return false, nil
			}
		}

		if len(fc.allObjects[vs.Name().Name]) > 0 {
			res, err := vs.Detections(ctx, img, nil)
			if err != nil {
				return false, err
			}

			match := fc.anyDetectionsMatch(vs.Name().Name, res)
			if match {
				fc.logger.Debugf("rejecting image with objects %v", res)
				return false, nil
			}
		}
	}

	for _, vs := range fc.otherVisionServices {
		if len(fc.allClassifications[vs.Name().Name]) > 0 {
			res, err := vs.Classifications(ctx, img, 100, nil)
			if err != nil {
				return false, err
			}

			match := fc.anyClassificationsMatch(vs.Name().Name, res)
			if match {
				fc.logger.Debugf("keeping image with classifications %v", res)
				fc.buf.MarkShouldSend(fc.conf.WindowSeconds)
				return true, nil
			}
		}

		if len(fc.allObjects[vs.Name().Name]) > 0 {
			res, err := vs.Detections(ctx, img, nil)
			if err != nil {
				return false, err
			}

			match := fc.anyDetectionsMatch(vs.Name().Name, res)
			if match {
				fc.logger.Debugf("keeping image with objects %v", res)
				fc.buf.MarkShouldSend(fc.conf.WindowSeconds)
				return true, nil
			}
		}

		if time.Now().Before(fc.buf.CaptureTill) {
			// send, but don't update captureTill
			return true, nil
		}
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
