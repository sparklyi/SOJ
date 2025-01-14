package main

import (
	"SOJ/initialize"
	"context"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func main() {
	initialize.InitConfig()
	f := initialize.InitServer()
	g := gin.Default()

	g.GET("/test", f.TestFunc)

	go f.Consume(context.Background())

	g.Run(viper.GetString("server.port"))

}
