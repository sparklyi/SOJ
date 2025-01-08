//go:build wireinject

package initialize

import (
	"SOJ/internal/api"
	"SOJ/utils/jwt"
	"github.com/google/wire"
)

func InitServer() *api.UserAPI {

	//wire会自动排列初始化顺序
	wire.Build(
		InitLogger,
		InitRedis,
		InitMiddleware,
		jwt.NewJWT,
		api.NewUserAPI,
	)
	return nil
}
