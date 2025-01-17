package service

import (
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"crypto/sha512"
	"errors"
	"fmt"
	"github.com/anaskhan96/go-password-encoder"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"strings"
)

type UserService struct {
	log  *zap.Logger
	repo *repository.UserRepository
	rs   *redis.Client
}

func NewUserService(log *zap.Logger, repo *repository.UserRepository, rs *redis.Client) *UserService {
	return &UserService{
		log:  log,
		repo: repo,
		rs:   rs,
	}
}

// EncryptPassword 密码加密(盐+hash)
func (us *UserService) EncryptPassword(raw string) string {
	option := &password.Options{SaltLen: 16, Iterations: 100, KeyLen: 32, HashFunction: sha512.New}
	salt, encode := password.Encode(raw, option)
	return fmt.Sprintf("pbkdf2-sha512$%s$%s", salt, encode)
}

// CheckPassword 密码检查
func (us *UserService) CheckPassword(raw, encrypt string) bool {
	//与加密保持一致
	option := &password.Options{SaltLen: 16, Iterations: 100, KeyLen: 32, HashFunction: sha512.New}
	list := strings.Split(encrypt, "$")
	return password.Verify(raw, list[1], list[2], option)
}

// Register 注册
func (us *UserService) Register(ctx *gin.Context, req *entity.Register) (*model.User, error) {

	check, err := us.rs.Get(ctx, req.Email).Result()
	//redis中不存在此键时返回redis.Nil
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	if check != req.Code {
		return nil, errors.New("验证码错误")
	}

	user := model.User{
		Email:    req.Email,
		Password: us.EncryptPassword(req.Password),
	}
	//转repository层
	if err = us.repo.Register(ctx, &user); err != nil {
		return nil, err
	}
	return &user, nil
}
