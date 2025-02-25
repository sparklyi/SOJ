package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"context"
	"errors"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	MysqlDuplicateError = 1062 //唯一索引错误
)

type UserRepository interface {
	CreateUserByEmail(ctx context.Context, user *model.User) error
	GetUserByEmail(ctx context.Context, email string) (*model.User, error)
	GetUserByID(ctx context.Context, id int) (*model.User, error)
	UpdateUserByID(ctx context.Context, user *model.User) error
	DeleteUserByID(ctx context.Context, id int) error
	DeleteUserByEmail(ctx context.Context, email string) error
	GetUserList(ctx context.Context, user *entity.UserInfo) ([]*model.User, error)
	UpdateUserByEmail(ctx context.Context, user *model.User) error
}

type user struct {
	log *zap.Logger
	db  *gorm.DB
	rs  *redis.Client
}

// NewUserRepository 依赖注入
func NewUserRepository(log *zap.Logger, db *gorm.DB, rs *redis.Client) UserRepository {
	return &user{
		log: log,
		db:  db,
		rs:  rs,
	}
}

// CreateUserByEmail 使用邮箱注册用户
func (ur *user) CreateUserByEmail(ctx context.Context, user *model.User) error {
	err := ur.db.WithContext(ctx).Create(user).Error
	var mysqlErr *mysql.MySQLError

	if errors.As(err, &mysqlErr) && mysqlErr.Number == MysqlDuplicateError {
		return errors.New("该邮箱已注册")
	}
	if err != nil {
		ur.log.Error("数据库异常", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil

}

// GetUserByEmail 根据邮箱获取用户信息
func (ur *user) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := ur.db.WithContext(ctx).Where("email = ?", email).First(u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("此邮箱未注册或已被封禁")
	} else if err != nil {
		ur.log.Error("数据库获取数据失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return u, nil
}

// GetUserByID 根据id获取用户信息
func (ur *user) GetUserByID(ctx context.Context, id int) (*model.User, error) {
	u := &model.User{}
	err := ur.db.WithContext(ctx).Where("id = ?", id).First(u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("ID不存在")
	} else if err != nil {
		ur.log.Error("数据库获取数据失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return u, nil
}

// UpdateUserByID 通过id更新用户信息
func (ur *user) UpdateUserByID(ctx context.Context, user *model.User) error {
	var mysqlErr *mysql.MySQLError
	err := ur.db.WithContext(ctx).Updates(user).Error
	if errors.As(err, &mysqlErr) && mysqlErr.Number == MysqlDuplicateError {
		return errors.New("唯一索引冲突")
	} else if err != nil {
		ur.log.Error("数据库获取数据失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// DeleteUserByID 根据id删除用户
func (ur *user) DeleteUserByID(ctx context.Context, id int) error {
	err := ur.db.WithContext(ctx).Where("id = ?", id).Delete(&model.User{}).Error
	if err != nil {
		ur.log.Error("数据库异常", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// DeleteUserByEmail 根据邮箱删除用户
func (ur *user) DeleteUserByEmail(ctx context.Context, email string) error {
	err := ur.db.WithContext(ctx).Where("email = ?", email).Delete(&model.User{}).Error
	if err != nil {
		ur.log.Error("数据库异常", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// GetUserList 获取用户列表(条件分页)
func (ur *user) GetUserList(ctx context.Context, user *entity.UserInfo) ([]*model.User, error) {
	db := ur.db.WithContext(ctx).Model(&model.User{})
	if user.ID != 0 {
		db = db.Where("id = ?", user.ID)
	}
	if user.Email != "" {
		db = db.Where("email = ?", user.Email)
	}
	if user.Tel != "" {
		db = db.Where("tel = ?", user.Tel)
	}
	if user.Username != "" {
		db = db.Where("username LIKE ?", "%"+user.Username+"%")
	}
	if user.Role != 0 {
		db = db.Where("role = ?", user.Role)
	}
	var us []*model.User
	err := db.Scopes(utils.Paginate(user.Page, user.PageSize)).Find(&us).Error
	if err != nil {
		ur.log.Error("获取用户列表失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return us, nil
}

// UpdateUserByEmail 使用email更新用户信息
func (ur *user) UpdateUserByEmail(ctx context.Context, user *model.User) error {
	var mysqlErr *mysql.MySQLError
	err := ur.db.WithContext(ctx).Where("email = ? ", user.Email).Updates(user).Error
	if errors.As(err, &mysqlErr) && mysqlErr.Number == MysqlDuplicateError {
		return errors.New("唯一索引冲突")
	} else if err != nil {
		ur.log.Error("数据库异常", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}
