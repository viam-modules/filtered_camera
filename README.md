# Viam filtered camera module

If your smart machine captures a lot of data, you can filter captured data to selectively store only the data that meets certain criteria.
This module allows you to filter image data by:

- Specified classification labels and required confidence scores
- Detected objects and their associated required confidence scores

This allows you to:

- Classify images and only sync images that have the required label
- Look for objects in an image and sync the images that have a certain object

This module also allows you to specify a time window for syncing the data captured in the N seconds before the capture criteria were met.

To add the filtered camera to your machine, navigate to the **CONFIGURE** tab of your machine’s page in [the Viam app](https://app.viam.com/).
[Add `camera` / `filtered-camera` to your machine](https://docs.viam.com/configure/#components).

## Configure your filtered camera

On the new component panel, copy and paste the following attribute template into your camera’s **Attributes** box.

```json
{
    "camera": "<your_camera_name>",
    "vision": "<your_vision_service_name>",
    "window_seconds": <time_window_for_capture>,
    "classifications": {
        "<label>": <confidence_score>,
        "<label>": <confidence_score>
    },
    "objects": {
        "<label>": <confidence_score>,
        "<label>": <confidence_score>
    }
}
```

Remove the "classifications" or "objects" section depending on if your ML model is a classifier or detector.

> [!NOTE]
> For more information, see [Configure a Machine](https://docs.viam.com/configure/).

### Attributes

The following attributes are available for `erh:camera:filtered-camera` bases:

| Name | Type | Inclusion | Description |
| ---- | ------ | ------------ | ----------- |
| `camera` | string | **Required** | The name of the camera to filter images for. |
| `vision` | string | **Required** | The vision service used for image classifications or detections. |
| `window_seconds` | float64 | Optional | The size of the time window (in seconds) during which images are buffered. When a condition is met, a confidence score for a detection/classification exceeds the required confidence score, the buffered images are stored, allowing us to see the photos taken in the N number of seconds preceding the condition being met. |
| `classifications` | float64 | Optional | A map of classification labels and the confidence scores required for filtering. Use this if the ML model behind your vision service is a classifier. You can find these labels by testing your vision service. |
| `objects` | float64 | Optional | A map of object detection labels and the confidence scores required for filtering. Use this if the ML model behind your vision service is a detector. You can find these labels by testing your vision service. |

### Example configurations

```json
{
    "camera": "my_camera",
    "vision": "my_red_car_detector",
    "windowSeconds": 5,
    "objects": {
        "red_car": 0.6
    }
}
```

## Local Development

To use the `filtered_camera` module, clone this repository to your
machine’s computer, navigate to the `module` directory, and run:

```go
go build
```

On your robot’s page in the [Viam app](https://app.viam.com/), enter
the [module’s executable path](/registry/create/#prepare-the-module-for-execution, then click
**Add module**.
The name must use only lowercase characters.
Then, click **Save** in the top right corner of the screen.

# Viam conditional camera module

If your smart machine captures a lot of data, you can conditionally capture data to selectively store only the data that meets certain criteria.

The conditional camera module allows you to filter image data by using a generic filter service as input.  When the input filter service returns "result": True, an image is provided from the underlying camera. When "result": False, no image is produced.

This allows you to:

- Develop a generic service that handles conditional logic from one or more component inputs and returns a boolean
- Data capture when the conditions established by your generic filter service are met

This module also allows you to specify a time window for syncing the data captured in the N seconds before the capture criteria were met.

## Configure your conditional camera

### Attributes

The following attributes are available for `viam:camera:conditional-camera` bases:

| Name | Type | Inclusion | Description |
| ---- | ------ | ------------ | ----------- |
| `camera` | string | **Required** | The name of the camera to filter images for. |
| `filter_service` | string | **Required** | The generic filter service used to determine whether to filter the image. |
| `window_seconds` | float64 | Optional | The size of the time window (in seconds) during which images are buffered. When a condition is met, a confidence score for a detection/classification exceeds the required confidence score, the buffered images are stored, allowing us to see the photos taken in the N number of seconds preceding the condition being met. |

On the new component panel, copy and paste the following attribute template into your camera’s **Attributes** box.

```json
{
    "camera": "<your_camera_name>",
    "filter_service": "<your_vision_service_name>",
    "window_seconds": <time_window_for_capture>
}
```

### Example configurations

```json
{
    "camera": "my_camera",
    "filter_service": "is_too_hot",
    "windowSeconds": 3
}
```

## Next Steps

- To test your camera, go to the [**CONTROL** tab](https://docs.viam.com/manage/fleet/robots/#control).
- To test that your camera's images are filtering correctly after [configuring data capture](https://docs.viam.com/services/data/capture/), [go to the **DATA** tab](https://docs.viam.com/data/view/).

## License

Copyright 2021-2023 Viam Inc. <br>
Apache 2.0
