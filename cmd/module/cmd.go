package main

import (
	"context"

	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/module"

	"github.com/erh/filtered_camera"
	"github.com/erh/filtered_camera/conditional_camera"
)

func main() {

	err := realMain()
	if err != nil {
		panic(err)
	}
}

func realMain() error {

	ctx := context.Background()
	logger := logging.NewDebugLogger("client")

	myMod, err := module.NewModuleFromArgs(ctx, logger)
	if err != nil {
		return err
	}

	err = myMod.AddModelFromRegistry(ctx, camera.API, filtered_camera.Model)
	if err != nil {
		return err
	}

	err = myMod.AddModelFromRegistry(ctx, camera.API, conditional_camera.Model)
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
