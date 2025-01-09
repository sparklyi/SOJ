//go:build wireinject

package initialize

import (
	"SOJ/internal/api"
	"SOJ/pkg/email"
	"SOJ/utils/jwt"
	"github.com/google/wire"
)

func InitServer() *api.UserAPI {

	//wire会自动排列初始化顺序
	wire.Build(
		InitLogger,
		InitRedis,
		InitMiddleware,
		jwt.New,
		email.New,
		api.NewUserAPI,
	)
	return nil
}
