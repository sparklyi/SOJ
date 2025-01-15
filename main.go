package main

import (
	"SOJ/initialize"
	"context"
	"github.com/spf13/viper"
)

func main() {
	initialize.InitConfig()
	f := initialize.InitServer()

	go f.EmailConsumer.Consume(context.Background())
	f.G.Run(viper.GetString("server.port"))

}
