package main

import (
	"SOJ/initialize"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func main() {
	initialize.InitConfig()
	f := initialize.InitServer()
	g := gin.Default()
	g.GET("/test", f.TestFunc)
	fmt.Println(viper.AllSettings())
	g.Run(viper.GetString("server.port"))

}
