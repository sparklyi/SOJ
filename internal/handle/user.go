package handle

import (
	"SOJ/internal/entity"
	"SOJ/internal/mq"
	"SOJ/utils/jwt"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"net/http"
)

type UserHandler struct {
	jwt        *jwt.JWT
	log        *zap.Logger
	middleware []gin.HandlerFunc
	email      *mq.EmailProducer
}

func NewUserHandler(log *zap.Logger, jwt *jwt.JWT, fc []gin.HandlerFunc, e *mq.EmailProducer) *UserHandler {
	return &UserHandler{
		jwt:        jwt,
		log:        log,
		middleware: fc,
		email:      e,
	}
}
func (u *UserHandler) TestFunc(ctx *gin.Context) {

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
	//if err = u.email.Send([]string{"sparkyi@qq.com"}, "test"); err != nil {
	//	panic(err)
	//}
	c := mq.EmailContent{Content: "test", Target: []string{"513254687@qq.com", "3026080028@qq.com"}}
	u.email.Send(ctx, c, 20)

}

func Register(ctx *gin.Context) {
	req := entity.Register{}
	if err := ctx.ShouldBind(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"msg": "请求无效"})
		return
	}

}
