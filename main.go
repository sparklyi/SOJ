package main

import (
	"SOJ/initialize"
)

func main() {
	initialize.InitConfig()
	logger := initialize.InitServer()
	defer logger.Sync()
	logger.Error("test")

}
