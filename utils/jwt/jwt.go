package jwt

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"time"
)

type AccessClaims struct {
	jwt.RegisteredClaims
	ID        int    //用户唯一id
	Auth      int    //用户权限
	UserAgent string //代理信息
}

type RefreshClaims struct {
	jwt.RegisteredClaims
	ID   int
	Auth int
}

type JWT struct {
	Rs            *redis.Client     //存储刷新长token
	Log           *zap.Logger       //日志
	Sign          jwt.SigningMethod //签名方法
	AccessSecret  []byte            //短token密钥
	RefreshSecret []byte            //长token密钥
	Issuer        string            //发行人
	AccessExpire  time.Duration     //短token过期时间
	RefreshExpire time.Duration     //长token过期时间
}

//依赖注入方法

func New(rs *redis.Client, log *zap.Logger) *JWT {
	return &JWT{
		Rs:            rs,
		Log:           log,
		Sign:          jwt.SigningMethodHS512,
		AccessSecret:  []byte(viper.GetString("jwt.access_secret")),
		RefreshSecret: []byte(viper.GetString("jwt.refresh_secret")),
		Issuer:        viper.GetString("jwt.issuer"),
		AccessExpire:  viper.GetDuration("jwt.access_expire") * time.Hour,
		RefreshExpire: viper.GetDuration("jwt.refresh_expire") * time.Hour,
	}
}

// CreateToken 创建长短token
func (j *JWT) CreateToken(ctx *gin.Context, id int, auth int) (string, string, error) {

	access, _ := j.CreateAccessToken(ctx, id, auth)
	refresh, err := j.CreateRefreshToken(id, auth)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// CreateAccessToken 创建短token
func (j *JWT) CreateAccessToken(ctx *gin.Context, id int, auth int) (string, error) {
	claims := &AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.Issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(j.AccessExpire)),
		},
		ID:        id,
		Auth:      auth,
		UserAgent: ctx.Request.UserAgent(),
	}
	token := jwt.NewWithClaims(j.Sign, claims)
	return token.SignedString(j.AccessSecret)
}

// CreateRefreshToken 创建长token
func (j *JWT) CreateRefreshToken(id int, auth int) (string, error) {
	claims := &RefreshClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.Issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(j.RefreshExpire)),
		},
		ID:   id,
		Auth: auth,
	}
	token := jwt.NewWithClaims(j.Sign, claims)
	return token.SignedString(j.RefreshSecret)

}

// ParseAccess 短token解析
func (j *JWT) ParseAccess(tokenString string) (*AccessClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &AccessClaims{}, func(token *jwt.Token) (interface{}, error) {
		return j.AccessSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*AccessClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, err

}

// ParseRefresh 长token解析
func (j *JWT) ParseRefresh(tokenString string) (*RefreshClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &RefreshClaims{}, func(token *jwt.Token) (interface{}, error) {
		return j.RefreshSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*RefreshClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, err

}

// BanToken jwt黑名单机制 将无效token存入redis
func (j *JWT) BanToken(ctx *gin.Context, token string) error {

	claims, err := j.ParseRefresh(token)
	if err != nil {
		return err
	}
	//加入redis黑名单
	remainder := claims.ExpiresAt.Time.Sub(time.Now())
	if err = j.Rs.Set(ctx, token, "", remainder).Err(); err != nil {
		return err
	}
	return nil
}

func (j *JWT) VerifyRefresh(ctx *gin.Context, token string) (bool, *RefreshClaims, error) {
	claims, err := j.ParseRefresh(token)
	if err != nil {
		return false, nil, err
	}
	exist, err := j.Rs.Exists(ctx, token).Result()
	if err != nil {
		j.Log.Error("redis读取长token失败", zap.Error(err))
		return false, nil, err
	}
	if exist != 0 {
		return false, nil, fmt.Errorf("token失效")
	}
	return true, claims, nil

}
