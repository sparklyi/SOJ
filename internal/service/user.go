package service

import (
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/mq"
	"SOJ/internal/repository"
	"SOJ/utils"
	"crypto/sha512"
	"errors"
	"fmt"
	"github.com/anaskhan96/go-password-encoder"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/tencentyun/cos-go-sdk-v5"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"mime/multipart"
	"strconv"
	"strings"
)

type UserService struct {
	log   *zap.Logger
	repo  *repository.UserRepository
	rs    *redis.Client
	cs    *cos.Client
	Email *EmailService
}

func NewUserService(log *zap.Logger, repo *repository.UserRepository, rs *redis.Client, cs *cos.Client, e *EmailService) *UserService {
	return &UserService{
		log:   log,
		repo:  repo,
		rs:    rs,
		cs:    cs,
		Email: e,
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

func (us *UserService) CheckCode(ctx *gin.Context, email string, code string) (bool, error) {
	check, err := us.rs.Get(ctx, email).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		us.log.Error("redis获取失败", zap.Error(err))
		return false, err
	}
	return check == code, nil

}

// Register 注册
func (us *UserService) Register(ctx *gin.Context, req *entity.Register) (*model.User, error) {

	if check, err := us.CheckCode(ctx, req.Email, req.Code); err != nil {
		return nil, err
	} else if !check {
		return nil, errors.New("验证码错误")
	}

	user := model.User{
		Email:    req.Email,
		Password: us.EncryptPassword(req.Password),
	}
	//转repository层
	if err := us.repo.CreateUserByEmail(ctx, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// LoginByEmail 使用邮箱登录
func (us *UserService) LoginByEmail(ctx *gin.Context, req *entity.LoginByEmail) (*model.User, error) {
	if check, err := us.CheckCode(ctx, req.Email, req.Code); err != nil {
		return nil, err
	} else if !check {
		return nil, errors.New("验证码错误")
	}
	return us.repo.GetUserByEmail(ctx, req.Email)
}

//
//func (us *UserService) LoginByPassword(ctx *gin.Context, req *entity.LoginByPassword) (*model.User, error) {
//
//}

// GetUserByID 通过ID获取用户信息
func (us *UserService) GetUserByID(ctx *gin.Context, id int) (*model.User, error) {
	user, err := us.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	//去敏
	user.Password = ""
	user.Email = ""
	return user, nil
}

// UploadAvatar 头像上传
func (us *UserService) UploadAvatar(ctx *gin.Context, fh *multipart.FileHeader, ID int) error {
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	t := strings.Split(fh.Filename, ".")
	if len(t) != 2 && t[1] != "jpg" && t[1] != "jpeg" && t[1] != "png" {
		return errors.New("文件不符")
	}
	//路径加密
	t[0] = utils.CryptoSHA1(strconv.Itoa(ID))
	name := "avatar/" + t[0] + "." + t[1]
	_, err = us.cs.Object.Put(ctx, name, f, nil)
	if err != nil {
		us.log.Error("上传失败", zap.Error(err))
		return errors.New("上传失败")
	}
	//将头像地址存入数据库
	user := model.User{
		Model:  gorm.Model{ID: uint(ID)},
		Avatar: us.cs.BaseURL.BucketURL.String() + "/" + name,
	}
	return us.repo.UpdateUserByID(ctx, &user)
}

// UpdatePassword 密码重置
func (us *UserService) UpdatePassword(ctx *gin.Context, req *entity.UpdatePassword) error {
	if check, err := us.CheckCode(ctx, req.Email, req.Code); err != nil {
		return err
	} else if !check {
		return errors.New("验证码错误")
	}
	//密码加密存入数据库
	user := &model.User{
		Email:    req.Email,
		Password: us.EncryptPassword(req.Password),
	}
	return us.repo.UpdateUserByEmail(ctx, user)
}

// GetUserList 获取用户列表
func (us *UserService) GetUserList(ctx *gin.Context, req *entity.UserInfo) (*[]model.User, error) {
	users, err := us.repo.GetUserList(ctx, req)
	if err != nil {
		return nil, err
	}
	//去敏
	for i := range *users {
		(*users)[i].Password = ""
	}
	return users, nil
}

// UpdateUserInfo 更新用户信息
func (us *UserService) UpdateUserInfo(ctx *gin.Context, req *entity.UserUpdate, admin bool) error {
	var user model.User
	if admin {
		user = model.User{
			Email: req.Email,
			Role:  req.Role,
		}
	}
	user.Username = req.Username
	user.ID = uint(req.ID)
	//修改角色权限为-1则直接删除用户
	if user.Role == -1 {
		return us.repo.DeleteUserByID(ctx, req.ID)
	}
	return us.repo.UpdateUserByID(ctx, &user)
}

// ResetPassword 重置密码
func (us *UserService) ResetPassword(ctx *gin.Context, email string) error {
	//生成随机密码
	pwd := utils.GenerateRandCode(10)
	err := us.repo.UpdateUserByEmail(ctx, &model.User{Email: email, Password: us.EncryptPassword(pwd)})
	if err != nil {
		return err
	}
	//向重置密码的用户发送新密码
	go func() {
		us.Email.emailProducer.Send(ctx, mq.EmailContent{
			Target:  []string{email},
			Subject: "密码重置",
			Content: "你的密码已被管理员重置,新密码为<a>" + pwd + "</a>,请妥善保管或及时修改",
			Code:    "",
		}, 0)
	}()
	return nil

}
