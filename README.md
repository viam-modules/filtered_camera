# Viam filtered camera module

If your smart machine captures a lot of data, you can filter captured data to selectively store only the data that meets certain criteria.
This module allows you to filter image data by:

- Specified classification labels and required confidence scores
- Detected objects and their associated required confidence scores

This allows you to:

- Classify images and only sync images that have the required label
- Look for objects in an image and sync the images that have a certain object

This module also allows you to specify a time window for syncing the data captured in the N seconds before the capture criteria were met.
You are also able to customize the time before and the time after the capture criteria, if you need that level of specficity.

To add the filtered camera to your machine, navigate to the **CONFIGURE** tab of your machine’s page in [the Viam app](https://app.viam.com/).
Add `camera` / `filtered-camera` to your machine.

## Configure your filtered camera

On the new component panel, copy and paste the following attribute template into your camera’s **Attributes** box.

```json
{
    "camera": "<your_camera_name>",
    "vision_services": [
        {
            "vision": <first_vision_service>,
            "classifications": ...,
            "objects": ...
        },
        {
            "vision": <second_vision_service>,
            "classifications": ...,
            "objects": ...
        }
    ],
    "window_seconds": <time_window_for_capture>,
    "window_seconds_before": <time_window_for_capture_before>,
    "window_seconds_after": <time_window_for_capture_after>,
    "image_frequency": <capture_rate_of_images_in_buffer>,
}
```

Remove the "classifications" or "objects" section depending on if your ML model is a classifier or detector.

> [!NOTE]
> For more information, see [Configure a Machine](https://docs.viam.com/operate/get-started/supported-hardware/#configure-hardware-on-your-machine).

### Attributes

The following attributes are available for `viam:camera:filtered-camera` bases:

| Name | Type | Inclusion | Description |
| ---- | ------ | ------------ | ----------- |
| `camera` | string | **Required** | The name of the camera to filter images for. |
| `vision_services` | list | **Required** | A list of 1 or more vision services used for image classifications or detections. |
| `vision` | string | **Required** | \*\***DEPRECATED**\*\* The vision service used for image classifications or detections. |
| `window_seconds_before` | float64 | Optional | The size of the time window (in seconds) before the condition is met, during which images are buffered. This allows you to see the photos taken in the specified number of seconds preceding the condition being met. |
| `window_seconds_after` | float64 | Optional | The size of the time window (in seconds) after the condition is met, during which images are buffered. This allows you to see the photos taken in the specified number of seconds after the condition being met. |
| `window_seconds` | float64 | Optional | The size of the time window (in seconds) during which images are buffered. When a condition is met, a confidence score for a detection/classification exceeds the required confidence score, the buffered images are stored, allowing us to see the photos taken in the specified number of seconds preceding and after the condition being met. If this value is set, the 'window_seconds_before' and 'window_seconds_after' variable must both be set to 0.0. |
| `image_frequency` | float64 | Optional | the frequency at which to place images into the buffer (in Hz). Default value is 1.0 Hz |
| `classifications` | float64 | Optional | \*\***DEPRECATED** Use `vision_services`\*\* A map of classification labels and the confidence scores required for filtering. Use this if the ML model behind your vision service is a classifier. You can find these labels by testing your vision service. |
| `objects` | float64 | Optional | \*\***DEPRECATED** use `vision_services` \*\* A map of object detection labels and the confidence scores required for filtering. Use this if the ML model behind your vision service is a detector. You can find these labels by testing your vision service. |

> [!WARNING]
> If a vision service has no specified classifications and/or objects, it won't trigger any data capture.

### Example configurations

```json
{
    "camera": "my_camera",
    "vision_services": [
        {
            "vision": "my_red_car_detector",
            "objects": {
                "red_car": 0.6
            }
        },
        {
            "vision": "my_cat_classifier",
            "classifications": {
                "cat": 0.6
            }
        }
    ],
    "window_seconds": 6,
    "image_frequency": 0.5,
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
| `window_seconds_before` | float64 | Optional | The size of the time window (in seconds) before the condition is met, during which images are buffered. This allows you to see the photos taken in the specified number of seconds preceding the condition being met. |
| `window_seconds_after` | float64 | Optional | The size of the time window (in seconds) after the condition is met, during which images are buffered. This allows you to see the photos taken in the specified number of seconds after the condition being met. |
| `window_seconds` | float64 | Optional | The size of the time window (in seconds) during which images are buffered. When a condition is met, a confidence score for a detection/classification exceeds the required confidence score, the buffered images are stored, allowing us to see the photos taken in the specified number of seconds preceding and after the condition being met. If this value is set, the 'window_seconds_before' and 'window_seconds_after' variable must both be set to 0.0. |

On the new component panel, copy and paste the following attribute template into your camera’s **Attributes** box.

```json
{
    "camera": "<your_camera_name>",
    "filter_service": "<your_vision_service_name>",
    "window_seconds": <time_window_for_capture>,
    "window_seconds_before": <time_window_for_capture_before>,
    "window_seconds_after": <time_window_for_capture_after>
}
```

### Example configurations

```json
{
    "camera": "my_camera",
    "filter_service": "is_too_hot",
    "window_seconds_before": 3,
    "window_seconds_after": 7,
}
```

## Next Steps

- To test your camera, go to the [**CONTROL** tab](https://docs.viam.com/manage/fleet/robots/#control).
- To test that your camera's images are filtering correctly after [configuring data capture](https://docs.viam.com/services/data/capture/), [go to the **DATA** tab](https://docs.viam.com/data/view/).

## License

Copyright 2021-2023 Viam Inc. <br>
Apache 2.0
