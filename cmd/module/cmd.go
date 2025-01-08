package main

import (
	"context"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"

	"github.com/erh/filtered_camera"
	"github.com/erh/filtered_camera/conditional_camera"
)

func main() {
	err := realMain(camera.API, filtered_camera.Model)
	if err != nil {
		panic(err)
	}
	err = realMain(camera.API, conditional_camera.Model)
	if err != nil {
		panic(err)
	}
}

func realMain(api resource.API, model resource.Model) error {

	ctx := context.Background()
	logger := logging.NewDebugLogger("client")

	myMod, err := module.NewModuleFromArgs(ctx, logger)
	if err != nil {
		return err
	}

	err = myMod.AddModelFromRegistry(ctx, api, model)
	if err != nil {
		return err
	}

	err = myMod.Start(ctx)
	defer myMod.Close(ctx)
	if err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}
