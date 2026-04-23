package filtered_camera

import (
	"context"

	"go.viam.com/rdk/data"
	"go.viam.com/rdk/resource"
)

var Family = resource.ModelNamespace("viam").WithFamily("camera")

func IsFromDataMgmt(ctx context.Context, extra map[string]interface{}) bool {
	if extra != nil && extra[data.FromDMString] == true {
		return true
	}

	return false
}
