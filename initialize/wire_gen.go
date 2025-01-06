// Code generated by Wire. DO NOT EDIT.

//go:generate go run -mod=mod github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

package initialize

import (
	"go.uber.org/zap"
)

// Injectors from wire.go:

func InitServer() *zap.Logger {
	logger := InitLogger()
	return logger
}
