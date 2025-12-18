package filtered_camera

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/data"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/pointcloud"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/spatialmath"
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
	Vision              string
	VisionServices      []VisionServiceConfig `json:"vision_services,omitempty"`
	WindowSeconds       int                   `json:"window_seconds"`
	ImageFrequency      float64               `json:"image_frequency"`
	WindowSecondsBefore int                   `json:"window_seconds_before"`
	WindowSecondsAfter  int                   `json:"window_seconds_after"`
	Debug               bool                  `json:"debug"`

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

func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.Camera == "" {
		return nil, nil, utils.NewConfigValidationFieldRequiredError(path, "camera")
	}

	if cfg.Vision == "" && cfg.VisionServices == nil {
		return nil, nil, utils.NewConfigValidationFieldRequiredError(path, "vision_services")
	} else if cfg.Vision != "" && cfg.VisionServices != nil {
		return nil, nil, utils.NewConfigValidationError(path, errors.New("cannot specify both vision and vision_services"))
	}

	if cfg.ImageFrequency < 0 {
		return nil, nil, utils.NewConfigValidationError(path, errors.New("image_frequency cannot be less than 0"))
	}

	if cfg.WindowSeconds == 0 && cfg.WindowSecondsBefore == 0 && cfg.WindowSecondsAfter == 0 {
		return nil, nil, utils.NewConfigValidationError(path,
			errors.New("window_seconds, window_seconds_after, and window_seconds_before cannot all be zero"))
	}

	if cfg.WindowSeconds < 0 || cfg.WindowSecondsBefore < 0 || cfg.WindowSecondsAfter < 0 {
		return nil, nil, utils.NewConfigValidationError(path,
			errors.New("none of window_seconds, window_seconds_after, or window_seconds_before can be negative"))
	} else if cfg.WindowSeconds > 0 && (cfg.WindowSecondsBefore > 0 || cfg.WindowSecondsAfter > 0) {
		return nil, nil, utils.NewConfigValidationError(path,
			errors.New("if window_seconds is set, window_seconds_before and window_seconds_after must not be"))
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
				return nil, nil, err
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

	return deps, nil, nil
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
			fc.buf = imagebuffer.NewImageBuffer(newConf.WindowSeconds, imageFreq, newConf.WindowSecondsBefore, newConf.WindowSecondsAfter, logger, newConf.Debug)

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
	images, meta, err := fc.cam.Images(ctx, nil, nil)
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
	ni, _, err := fc.images(ctx, nil, extra, true) // true indicates single image mode
	if err != nil {
		return nil, camera.ImageMetadata{}, err
	}

	return ImagesToImage(ctx, ni)
}

func (fc *filteredCamera) Images(ctx context.Context, filterSourceNames []string, extra map[string]interface{}) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	return fc.images(ctx, filterSourceNames, extra, false) // false indicates multiple images mode
}

// getBufferedImages returns images from the ToSend buffer depending on the image mode.
// single image just returns the first image in the queue, while otherwise it returns the whole buffer
// if ToSend is empty, returns false
func (fc *filteredCamera) getBufferedImages(singleImageMode bool) ([]camera.NamedImage, resource.ResponseMetadata, bool) {
	if singleImageMode {
		if x, ok := fc.buf.PopFirstToSend(); ok {
			return x.Imgs, x.Meta, true
		}
	} else {
		if allImages, batchMeta, ok := fc.buf.PopAllToSend(); ok {
			return allImages, batchMeta, true
		}
	}
	// ToSend buffer is empty - no images to capture
	return nil, resource.ResponseMetadata{}, false
}

// images checks to see if the trigger is fulfilled or inhibited, and sets the flag to send images
// It then returns the next image or images present in the ToSend buffer back to the client / data manager
// singleImageMode indicates if this is called from Image() (true) or Images() (false)
func (fc *filteredCamera) images(ctx context.Context, filterSourceNames []string, extra map[string]interface{}, singleImageMode bool) ([]camera.NamedImage, resource.ResponseMetadata, error) {
	// Always call underlying camera to get fresh images
	images, meta, err := fc.cam.Images(ctx, filterSourceNames, extra)
	if err != nil {
		return images, meta, err
	}

	if !IsFromDataMgmt(ctx, extra) {
		return images, meta, nil
	}

	// If we're still within an active capture window, skip filter checks
	if fc.buf.IsWithinCaptureWindow(meta.CapturedAt) {
		if fc.conf.Debug {
			fc.logger.Infow("Skipping filter checks",
				"method", "images",
				"singleImageMode", singleImageMode,
				"capturedAt", meta.CapturedAt,
				"withinCaptureWindow", true)
		}
		if bufferedImages, bufferedMeta, ok := fc.getBufferedImages(singleImageMode); ok {
			return bufferedImages, bufferedMeta, nil
		}
		// If no buffered images, return current image (we're in capture mode)
		// Apply timestamp to current images for consistency
		timestampedImages := imagebuffer.TimestampImagesToNames(images, meta)
		return timestampedImages, meta, nil
	}

	if fc.conf.Debug {
		fc.logger.Infow("Running filter checks",
			"method", "images",
			"singleImageMode", singleImageMode,
			"capturedAt", meta.CapturedAt,
			"withinCaptureWindow", false)
	}

	// We're outside capture window, so run filter checks to potentially start a new capture
	for _, img := range images {
		// method fc.shouldSend will return true if a filter passes (and inhibit doesn't)
		shouldSend, err := fc.shouldSend(ctx, img, meta.CapturedAt)
		if err != nil {
			return nil, meta, err
		}
		if shouldSend {
			// this updates the CaptureTill time to be further in the future
			fc.buf.MarkShouldSend(meta.CapturedAt)

			if bufferedImages, bufferedMeta, ok := fc.getBufferedImages(singleImageMode); ok {
				return bufferedImages, bufferedMeta, nil
			}

			// Don't return the trigger image to maintain chronological order
			// Represents the edge case where triggering is happening faster than the buffer can be filled
			return nil, meta, data.ErrNoCaptureToStore
		}
	}
	// No triggers met and we're outside capture window, but check if we have buffered images from previous triggers
	if bufferedImages, bufferedMeta, ok := fc.getBufferedImages(singleImageMode); ok {
		return bufferedImages, bufferedMeta, nil
	}

	// ToSend buffer is empty - no images to capture
	return nil, meta, data.ErrNoCaptureToStore
}

func (fc *filteredCamera) shouldSend(ctx context.Context, namedImg camera.NamedImage, now time.Time) (bool, error) {
	img, err := namedImg.Image(ctx)
	if err != nil {
		return false, err
	}
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

func (fc *filteredCamera) NextPointCloud(ctx context.Context, _ map[string]interface{}) (pointcloud.PointCloud, error) {
	return nil, fmt.Errorf("filteredCamera doesn't support pointclouds yet")
}

func (fc *filteredCamera) Geometries(ctx context.Context, extra map[string]interface{}) ([]spatialmath.Geometry, error) {
	return nil, errors.New("unimplemented")
}

func (fc *filteredCamera) Properties(ctx context.Context) (camera.Properties, error) {
	p, err := fc.cam.Properties(ctx)
	if err == nil {
		p.SupportsPCD = false
	}
	return p, err
}
