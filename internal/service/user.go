package service

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/mq/producer"
	"SOJ/internal/repository"
	"SOJ/utils"
	"SOJ/utils/captcha"
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
	"time"
)

type UserService interface {
	EncryptPassword(raw string) string
	CheckPassword(raw string, encrypt string) bool
	CheckCode(ctx *gin.Context, email string, code string) (bool, error)
	Register(ctx *gin.Context, req *entity.Register) (*model.User, error)
	LoginByEmail(ctx *gin.Context, req *entity.LoginByEmail) (*model.User, error)
	LoginByPassword(ctx *gin.Context, req *entity.LoginByPassword) (*model.User, error)
	GetUserByID(ctx *gin.Context, id int) (*model.User, error)
	UploadAvatar(ctx *gin.Context, fh *multipart.FileHeader, ID int) error
	UpdatePassword(ctx *gin.Context, req *entity.UpdatePassword) error
	GetUserList(ctx *gin.Context, req *entity.UserInfo) ([]*model.User, error)
	UpdateUserInfo(ctx *gin.Context, req *entity.UserUpdate, admin bool) error
	ResetPassword(ctx *gin.Context, email string) error
	DeleteByID(ctx *gin.Context, id int) error
}

type user struct {
	log     *zap.Logger
	repo    repository.UserRepository
	rs      *redis.Client
	cs      *cos.Client
	email   *producer.Email
	captcha *captcha.Captcha
}

func NewUserService(log *zap.Logger, repo repository.UserRepository, rs *redis.Client, cs *cos.Client, e *producer.Email, c *captcha.Captcha) UserService {
	return &user{
		log:     log,
		repo:    repo,
		rs:      rs,
		cs:      cs,
		email:   e,
		captcha: c,
	}
}

// EncryptPassword 密码加密(盐+hash)
func (us *user) EncryptPassword(raw string) string {
	option := &password.Options{SaltLen: 16, Iterations: 100, KeyLen: 32, HashFunction: sha512.New}
	salt, encode := password.Encode(raw, option)
	return fmt.Sprintf("pbkdf2-sha512$%s$%s", salt, encode)
}

// CheckPassword 密码检查
func (us *user) CheckPassword(raw, encrypt string) bool {
	//与加密保持一致
	option := &password.Options{SaltLen: 16, Iterations: 100, KeyLen: 32, HashFunction: sha512.New}
	list := strings.Split(encrypt, "$")
	return password.Verify(raw, list[1], list[2], option)
}

func (us *user) CheckCode(ctx *gin.Context, email string, code string) (bool, error) {
	check, err := us.rs.Get(ctx, email).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		us.log.Error("redis获取失败", zap.Error(err))
		return false, err
	}
	return check == code, nil

}

// Register 注册
func (us *user) Register(ctx *gin.Context, req *entity.Register) (*model.User, error) {

	if check, err := us.CheckCode(ctx, req.Email, req.Code); err != nil {
		return nil, err
	} else if !check {
		return nil, errors.New(constant.CodeError)
	}

	u := model.User{
		Username: "用户" + req.Email,
		Email:    req.Email,
		Password: us.EncryptPassword(req.Password),
	}
	//转repository层
	if err := us.repo.CreateUserByEmail(ctx, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// LoginByEmail 使用邮箱登录
func (us *user) LoginByEmail(ctx *gin.Context, req *entity.LoginByEmail) (*model.User, error) {
	if check, err := us.CheckCode(ctx, req.Email, req.Code); err != nil {
		return nil, err
	} else if !check {
		return nil, errors.New(constant.CodeError)
	}
	return us.repo.GetUserByEmail(ctx, req.Email)
}

// LoginByPassword 密码登录
func (us *user) LoginByPassword(ctx *gin.Context, req *entity.LoginByPassword) (*model.User, error) {
	//验证码验证
	if !us.captcha.Verify(req.CaptchaID, req.Captcha, true) {
		return nil, errors.New(constant.CodeError)
	}

	//检查是否缓存在redis中
	checkEmail := "password:" + req.Email
	seg := ":"
	pass, err := us.rs.Get(ctx, checkEmail).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	//缓存过密码或者特殊值
	if list := strings.Split(pass, seg); len(list) == 2 {
		if !us.CheckPassword(req.Password, list[1]) {
			return nil, errors.New(constant.PasswordError)
		} else {
			id, _ := strconv.Atoi(list[0])
			u := model.User{Model: gorm.Model{ID: uint(id)}}
			return &u, nil
		}
	}
	//未缓存在redis中
	u, err := us.repo.GetUserByEmail(ctx, req.Email)
	//未找到对应邮箱
	if err != nil {
		if err.Error() != constant.ServerError {
			//设置随机值 防止恶意调用
			us.rs.Set(ctx, checkEmail,
				"0"+seg+us.EncryptPassword(utils.GenerateRandCode(20, false)),
				time.Minute*constant.ExpireTime)
		}
		return nil, err
	}
	//邮箱存在, 缓存真实密码
	us.rs.Set(ctx, checkEmail, fmt.Sprintf("%v:%s", u.ID, u.Password), time.Minute*constant.ExpireTime)
	if !us.CheckPassword(req.Password, u.Password) {
		return nil, errors.New(constant.PasswordError)
	}
	return u, nil
}

// GetUserByID 通过ID获取用户信息
func (us *user) GetUserByID(ctx *gin.Context, id int) (*model.User, error) {
	u, err := us.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	//去敏
	u.Password = ""
	u.Email = ""
	return u, nil
}

// UploadAvatar 头像上传
func (us *user) UploadAvatar(ctx *gin.Context, fh *multipart.FileHeader, ID int) error {
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	t := strings.Split(fh.Filename, ".")
	if len(t) != 2 && t[1] != "jpg" && t[1] != "jpeg" && t[1] != "png" {
		return errors.New(constant.ParamError)
	}
	//路径加密
	t[0] = utils.CryptoSHA1(strconv.Itoa(ID))
	name := "avatar/" + t[0] + "." + t[1]
	_, err = us.cs.Object.Put(ctx, name, f, nil)
	if err != nil {
		us.log.Error("上传失败", zap.Error(err))
		return errors.New(constant.UploadError)
	}
	//将头像地址存入数据库
	u := model.User{
		Model:  gorm.Model{ID: uint(ID)},
		Avatar: us.cs.BaseURL.BucketURL.String() + "/" + name,
	}
	return us.repo.UpdateUserByID(ctx, &u)
}

// UpdatePassword 密码重置
func (us *user) UpdatePassword(ctx *gin.Context, req *entity.UpdatePassword) error {
	if check, err := us.CheckCode(ctx, req.Email, req.Code); err != nil {
		return err
	} else if !check {
		return errors.New(constant.CodeError)
	}
	//密码加密存入数据库
	u := &model.User{
		Email:    req.Email,
		Password: us.EncryptPassword(req.Password),
	}
	return us.repo.UpdateUserByEmail(ctx, u)
}

// GetUserList 获取用户列表
func (us *user) GetUserList(ctx *gin.Context, req *entity.UserInfo) ([]*model.User, error) {
	users, err := us.repo.GetUserList(ctx, req)
	if err != nil {
		return nil, err
	}
	//去敏
	for i := range users {
		users[i].Password = ""
	}
	return users, nil
}

// UpdateUserInfo 更新用户信息
func (us *user) UpdateUserInfo(ctx *gin.Context, req *entity.UserUpdate, admin bool) error {
	var u model.User
	if admin {
		u = model.User{
			Email: req.Email,
			Role:  req.Role,
		}
	}
	u.Username = req.Username
	u.ID = uint(req.ID)
	//修改角色权限为-1则直接删除用户
	if u.Role == constant.BanLevel {
		return us.repo.DeleteUserByID(ctx, req.ID)
	}
	return us.repo.UpdateUserByID(ctx, &u)
}

// ResetPassword 重置密码
func (us *user) ResetPassword(ctx *gin.Context, email string) error {
	//生成随机密码
	pwd := utils.GenerateRandCode(10, false)
	err := us.repo.UpdateUserByEmail(ctx, &model.User{Email: email, Password: us.EncryptPassword(pwd)})
	if err != nil {
		return err
	}
	//向重置密码的用户发送新密码
	go func() {
		us.email.Send(ctx, producer.EmailContent{
			Target:  []string{email},
			Subject: "密码重置",
			Content: "你的密码已被管理员重置,新密码为<a>" + pwd + "</a>,请妥善保管或及时修改",
			Code:    "",
		}, 0)
	}()
	return nil

}

// DeleteByID 删除用户
func (us *user) DeleteByID(ctx *gin.Context, id int) error {
	return us.repo.DeleteUserByID(ctx, id)
}
