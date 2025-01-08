package api

import (
	"SOJ/utils/jwt"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http"
)

type UserAPI struct {
	jwt        *jwt.JWT
	log        *zap.Logger
	middleware []gin.HandlerFunc
}

func NewUserAPI(log *zap.Logger, jwt *jwt.JWT, fc []gin.HandlerFunc) *UserAPI {
	return &UserAPI{
		jwt:        jwt,
		log:        log,
		middleware: fc,
	}
}
func (u *UserAPI) TestFunc(ctx *gin.Context) {

	//颁发token

	access, refresh, err := u.jwt.CreateToken(ctx, 1, 1)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{})
		return
	}
	//ctx.JSON(http.StatusOK, gin.H{
	//	"SOJ-Access-Token":  access,
	//	"SOJ-Refresh-Token": refresh,
	//})
	fmt.Println(access)
	fmt.Println(refresh)

	// 长token手动无效，加入redis黑名单
	err = u.jwt.BanToken(ctx, refresh)
	if err != nil {
		panic(err)
	}

}
