//go:build wireinject

package initialize

import (
	"github.com/google/wire"
	"go.uber.org/zap"
)

func InitServer() *zap.Logger {
	wire.Build(InitLogger)
	return nil
}
