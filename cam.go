package filtered_camera

import (
	"context"
	"errors"
	"fmt"
	"image"
	"time"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/vision/classification"
	"go.viam.com/rdk/vision/objectdetection"
	"go.viam.com/utils"

	imagebuffer "github.com/viam-modules/filtered_camera/image_buffer"
)

var Model = Family.WithModel("filtered-camera")

const defaultImageFreq = 1.0

type Config struct {
	Camera string
	// Deprecated: use VisionServices instead
	Vision         string
	VisionServices []VisionServiceConfig `json:"vision_services,omitempty"`
	WindowSeconds  int                   `json:"window_seconds"`
	ImageFrequency float64               `json:"image_frequency"`

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

	if cfg.ImageFrequency < 0 {
		return nil, utils.NewConfigValidationError(path, errors.New("image_frequency cannot be less than 0"))
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
					fc.acceptedClassifications = make(map[string]map[string]float64)
					fc.acceptedClassifications[newConf.Vision] = newConf.Classifications
				}
				if newConf.Objects != nil {
					fc.acceptedObjects = make(map[string]map[string]float64)
					fc.acceptedObjects[newConf.Vision] = newConf.Objects
				}
			} else {
				fc.inhibitors = []vision.Service{}
				fc.otherVisionServices = []vision.Service{}
				fc.inhibitedClassifications = make(map[string]map[string]float64)
				fc.acceptedClassifications = make(map[string]map[string]float64)
				fc.inhibitedObjects = make(map[string]map[string]float64)
				fc.acceptedObjects = make(map[string]map[string]float64)
				for _, vs := range newConf.VisionServices {
					visionService, err := vision.FromDependencies(deps, vs.Vision)
					if err != nil {
						return nil, err
					}

					if vs.Inhibit {
						fc.inhibitors = append(fc.inhibitors, visionService)
						if vs.Classifications != nil {
							fc.inhibitedClassifications[vs.Vision] = vs.Classifications
						}
						if vs.Objects != nil {
							fc.inhibitedObjects[vs.Vision] = vs.Objects
						}
					} else {
						fc.otherVisionServices = append(fc.otherVisionServices, visionService)
						if vs.Classifications != nil {
							fc.acceptedClassifications[vs.Vision] = vs.Classifications
						}
						if vs.Objects != nil {
							fc.acceptedObjects[vs.Vision] = vs.Objects
						}
					}
				}
			}
			fc.acceptedStats.startTime = time.Now()
			fc.rejectedStats.startTime = time.Now()

			// Initialize the image buffer
			imageFreq := newConf.ImageFrequency
			if imageFreq == 0 {
				imageFreq = defaultImageFreq
			}
			fc.buf = imagebuffer.NewImageBuffer(newConf.WindowSeconds, imageFreq)

			// Initialize background image capture worker
			fc.backgroundWorkers = utils.NewStoppableWorkerWithTicker(
				time.Duration(1000.0/imageFreq)*time.Millisecond,
				func(ctx context.Context) {
					fc.captureImageInBackground(ctx)
				},
			)

			return fc, nil
		},
	})
}

type filteredCamera struct {
	resource.AlwaysRebuild

	name   resource.Name
	conf   *Config
	logger logging.Logger

	cam                      camera.Camera
	buf                      *imagebuffer.ImageBuffer
	backgroundWorkers        *utils.StoppableWorkers
	inhibitors               []vision.Service
	otherVisionServices      []vision.Service
	inhibitedClassifications map[string]map[string]float64
	acceptedClassifications  map[string]map[string]float64
	inhibitedObjects         map[string]map[string]float64
	acceptedObjects          map[string]map[string]float64
	acceptedStats            imageStats
	rejectedStats            imageStats
}

type imageStats struct {
	total     int
	breakdown map[string]int
	startTime time.Time
}

func (is *imageStats) update(visionService string) {
	is.total++
	if is.breakdown == nil {
		is.breakdown = make(map[string]int)
	}
	if _, ok := is.breakdown[visionService]; !ok {
		is.breakdown[visionService] = 1
		return
	}
	is.breakdown[visionService]++
}

func (fc *filteredCamera) formatStats() map[string]interface{} {
	stats := make(map[string]interface{})
	stats["accepted"] = make(map[string]interface{})
	stats["rejected"] = make(map[string]interface{})

	if acceptedStats, ok := stats["accepted"].(map[string]interface{}); !ok {
		fc.logger.Errorf("failed to get stats")
		return nil
	} else {
		acceptedStats["total"] = fc.acceptedStats.total
		acceptedStats["vision"] = fc.acceptedStats.breakdown
	}
	if rejectedStats, ok := stats["rejected"].(map[string]interface{}); !ok {
		fc.logger.Errorf("failed to get stats")
		return nil
	} else {
		rejectedStats["total"] = fc.rejectedStats.total
		rejectedStats["vision"] = fc.rejectedStats.breakdown
	}

	stats["start_time"] = fc.acceptedStats.startTime.Format(time.RFC1123)
	return stats
}

func (fc *filteredCamera) anyClassificationsMatch(visionService string, cs []classification.Classification, inhibit bool) (bool, classification.Classification) {
	for _, c := range cs {
		if fc.classificationMatches(visionService, c, inhibit) {
			return true, c
		}
	}
	return false, nil
}

func (fc *filteredCamera) classificationMatches(visionService string, c classification.Classification, inhibit bool) bool {
	var allClassifications map[string]map[string]float64
	if inhibit {
		allClassifications = fc.inhibitedClassifications
	} else {
		allClassifications = fc.acceptedClassifications
	}

	min, has := allClassifications[visionService][c.Label()]
	if has && c.Score() > min {
		return true
	}

	min, has = allClassifications[visionService]["*"]
	if has && c.Score() > min {
		return true
	}

	return false
}

func (fc *filteredCamera) anyDetectionsMatch(visionService string, ds []objectdetection.Detection, inhibit bool) (bool, objectdetection.Detection) {
	for _, d := range ds {
		if fc.detectionMatches(visionService, d, inhibit) {
			return true, d
		}
	}

	return false, nil
}

func (fc *filteredCamera) detectionMatches(visionService string, d objectdetection.Detection, inhibit bool) bool {
	var allDetections map[string]map[string]float64
	if inhibit {
		allDetections = fc.inhibitedObjects
	} else {
		allDetections = fc.acceptedObjects
	}

	min, has := allDetections[visionService][d.Label()]
	if has && d.Score() > min {
		return true
	}

	min, has = allDetections[visionService]["*"]
	if has && d.Score() > min {
		return true
	}

	return false
}

func (fc *filteredCamera) Name() resource.Name {
	return fc.name
}

func (fc *filteredCamera) Close(ctx context.Context) error {
	if fc.backgroundWorkers != nil {
		fc.backgroundWorkers.Stop()
	}
	return nil
}

func (fc *filteredCamera) captureImageInBackground(ctx context.Context) {
	images, meta, err := fc.cam.Images(ctx)
	if err != nil {
		fc.logger.Debugf("Error capturing image in background: %v", err)
		return
	}
	now := meta.CapturedAt
	fc.buf.StoreImages(images, meta, now)
}

func (fc *filteredCamera) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	return fc.formatStats(), nil
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

// images checks to see if the trigger is fulfilled or inhibited, and sets the flag to send images
// It then returns the next image present in the ToSend buffer back to the client / data manager
func (fc *filteredCamera) images(ctx context.Context, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	images, meta, err := fc.cam.Images(ctx)
	if err != nil {
		return images, meta, err
	}

	if !IsFromDataMgmt(ctx, extra) {
		return images, meta, nil
	}

	for _, img := range images {
		// method fc.shouldSend will return true if a filter passes (and inhibit doesn't)
		shouldSend, err := fc.shouldSend(ctx, img.Image, meta.CapturedAt)
		if err != nil {
			return nil, meta, err
		}
		if shouldSend {
			// this updates the CaptureTill time to be further in the future
			fc.buf.MarkShouldSend(meta.CapturedAt)
			fc.buf.CacheImages(images)

			if x, ok := fc.buf.PopFirstToSend(); ok {
				return x.Imgs, x.Meta, nil
			}

			return images, meta, nil
		}
	}
	// background loop will fill ToSend buffer as long as within CaptureTill time
	if x, ok := fc.buf.PopFirstToSend(); ok {
		return x.Imgs, x.Meta, nil
	}
	return nil, meta, data.ErrNoCaptureToStore
}

func (fc *filteredCamera) shouldSend(ctx context.Context, img image.Image, now time.Time) (bool, error) {
	// inhibitors are first priority
	for _, vs := range fc.inhibitors {
		if len(fc.inhibitedClassifications[vs.Name().Name]) > 0 {
			res, err := vs.Classifications(ctx, img, 100, nil)
			if err != nil {
				fc.logger.Debugf("error getting inhibited classifications")
				return false, err
			}

			match, label := fc.anyClassificationsMatch(vs.Name().Name, res, true)
			if match {
				fc.logger.Debugf("rejecting image with classifications %v", res)
				fc.rejectedStats.update(label.Label())
				return false, nil
			}
		}

		if len(fc.inhibitedObjects[vs.Name().Name]) > 0 {
			res, err := vs.Detections(ctx, img, nil)
			if err != nil {
				fc.logger.Debugf("error getting inhibited detections")
				return false, err
			}

			match, label := fc.anyDetectionsMatch(vs.Name().Name, res, true)
			if match {
				fc.logger.Debugf("rejecting image with objects %v", res)
				fc.rejectedStats.update(label.Label())
				return false, nil
			}
		}
	}

	for _, vs := range fc.otherVisionServices {
		if len(fc.acceptedClassifications[vs.Name().Name]) > 0 {
			res, err := vs.Classifications(ctx, img, 100, nil)
			if err != nil {
				fc.logger.Debugf("error getting non-inhibited classifications")
				return false, err
			}

			match, label := fc.anyClassificationsMatch(vs.Name().Name, res, false)
			if match {
				fc.logger.Debugf("keeping image with classifications %v", res)
				fc.acceptedStats.update(label.Label())
				return true, nil
			}
		}

		if len(fc.acceptedObjects[vs.Name().Name]) > 0 {
			res, err := vs.Detections(ctx, img, nil)
			if err != nil {
				fc.logger.Debugf("error getting non-inhibited detections")
				return false, err
			}

			match, label := fc.anyDetectionsMatch(vs.Name().Name, res, false)
			if match {
				fc.logger.Debugf("keeping image with objects %v", res)
				fc.acceptedStats.update(label.Label())
				return true, nil
			}
		}
	}
	if len(fc.otherVisionServices) == 0 {
		fc.acceptedStats.update("no vision services triggered")
		fc.logger.Debugf("defaulting to true")
		return true, nil
	}
	fc.rejectedStats.update("no vision services triggered")
	fc.logger.Debugf("defaulting to false")
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
