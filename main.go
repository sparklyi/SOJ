package main

import (
	"SOJ/initialize"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func main() {
	initialize.InitConfig()
	f := initialize.InitServer()
	g := gin.Default()

	g.GET("/test", f.TestFunc)

	g.Run(viper.GetString("server.port"))

}
