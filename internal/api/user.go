package api

import (
	"SOJ/pkg/email"
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
	email      *email.Email
}

func NewUserAPI(log *zap.Logger, jwt *jwt.JWT, fc []gin.HandlerFunc, e *email.Email) *UserAPI {
	return &UserAPI{
		jwt:        jwt,
		log:        log,
		middleware: fc,
		email:      e,
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
	//err = u.jwt.BanToken(ctx, refresh)
	//if err != nil {
	//	panic(err)
	//}

	//发送邮件
	if err = u.email.Send([]string{"sparkyi@qq.com"}, "test"); err != nil {
		panic(err)
	}

}
