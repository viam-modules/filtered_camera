package main

import (
	"context"
	"os"

	goutils "go.viam.com/utils"

	"go.viam.com/rdk/config"
	"go.viam.com/rdk/grpc"
	"go.viam.com/rdk/logging"
	robotimpl "go.viam.com/rdk/robot/impl"
	"go.viam.com/rdk/robot/web"
	"go.viam.com/utils/rpc"

	_ "github.com/viam-modules/filtered_camera"
)

func main() {
	err := realMain()
	if err != nil {
		panic(err)
	}
}
func realMain() error {

	ctx := context.Background()
	logger := logging.NewDebugLogger("remotetest")

	conf, err := config.ReadLocalConfig(os.Args[1], logger)
	if err != nil {
		return err
	}

	conf.Network.BindAddress = "0.0.0.0:8082"
	if err := conf.Network.Validate(""); err != nil {
		return err
	}

	var appConn rpc.ClientConn
	if conf.Cloud != nil && conf.Cloud.AppAddress != "" {
		appConn, err = grpc.NewAppConn(ctx, conf.Cloud.AppAddress, conf.Cloud.Secret, conf.Cloud.ID, logger)
		if err != nil {
			return nil
		}

		defer goutils.UncheckedErrorFunc(appConn.Close)
	}

	myRobot, err := robotimpl.New(ctx, conf, appConn, logger)
	if err != nil {
		return err
	}

	return web.RunWebWithConfig(ctx, myRobot, conf, logger)
}
