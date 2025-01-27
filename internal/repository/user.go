package repository

import (
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	MysqlDuplicateError = 1062 //唯一索引错误
)

type UserRepository struct {
	log *zap.Logger
	db  *gorm.DB
	rs  *redis.Client
}

// NewUserRepository 依赖注入
func NewUserRepository(log *zap.Logger, db *gorm.DB, rs *redis.Client) *UserRepository {
	return &UserRepository{
		log: log,
		db:  db,
		rs:  rs,
	}
}

// CreateUserByEmail 使用邮箱注册用户
func (ur *UserRepository) CreateUserByEmail(c *gin.Context, user *model.User) error {
	err := ur.db.WithContext(c).Create(user).Error
	var mysqlErr *mysql.MySQLError

	if errors.As(err, &mysqlErr) && mysqlErr.Number == MysqlDuplicateError {
		return errors.New("该邮箱已注册")
	}
	if err != nil {
		ur.log.Error("数据库异常", zap.Error(err))
		return errors.New("服务器异常")
	}
	return nil

}

// GetUserByEmail 根据邮箱获取用户信息
func (ur *UserRepository) GetUserByEmail(c *gin.Context, email string) (*model.User, error) {
	user := &model.User{}
	err := ur.db.WithContext(c).Where("email = ?", email).First(user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("此邮箱未注册或已被封禁")
	} else if err != nil {
		ur.log.Error("数据库获取数据失败", zap.Error(err))
		return nil, errors.New("内部错误")
	}
	return user, nil
}

// GetUserByID 根据id获取用户信息
func (ur *UserRepository) GetUserByID(c *gin.Context, id int) (*model.User, error) {
	user := &model.User{}
	err := ur.db.WithContext(c).Where("id = ?", id).First(user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("ID不存在")
	} else if err != nil {
		ur.log.Error("数据库获取数据失败", zap.Error(err))
		return nil, errors.New("内部错误")
	}
	return user, nil
}

// UpdateUserByID 通过id更新用户信息
func (ur *UserRepository) UpdateUserByID(c *gin.Context, user *model.User) error {
	var mysqlErr *mysql.MySQLError
	err := ur.db.WithContext(c).Updates(user).Error
	if errors.As(err, &mysqlErr) && mysqlErr.Number == MysqlDuplicateError {
		return errors.New("唯一索引冲突")
	} else if err != nil {
		ur.log.Error("数据库获取数据失败", zap.Error(err))
		return errors.New("内部错误")
	}
	return nil
}

// DeleteUserByID 根据id删除用户
func (ur *UserRepository) DeleteUserByID(c *gin.Context, id int) error {
	err := ur.db.WithContext(c).Where("id = ?", id).Delete(&model.User{}).Error
	if err != nil {
		ur.log.Error("数据库异常", zap.Error(err))
		return errors.New("内部错误")
	}
	return nil
}

// DeleteUserByEmail 根据邮箱删除用户
func (ur *UserRepository) DeleteUserByEmail(c *gin.Context, email string) error {
	err := ur.db.WithContext(c).Where("email = ?", email).Delete(&model.User{}).Error
	if err != nil {
		ur.log.Error("数据库异常", zap.Error(err))
		return errors.New("内部错误")
	}
	return nil
}

// GetUserList 获取用户列表(条件分页)
func (ur *UserRepository) GetUserList(c *gin.Context, user *entity.UserInfo) (*[]model.User, error) {
	db := ur.db.WithContext(c).Model(&model.User{})
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
	var us []model.User
	err := db.Scopes(utils.Paginate(user.Page, user.PageSize)).Find(&us).Error
	if err != nil {
		ur.log.Error("获取用户列表失败", zap.Error(err))
		return nil, errors.New("内部错误")
	}
	return &us, nil
}

// UpdateUserByEmail 使用email更新用户信息
func (ur *UserRepository) UpdateUserByEmail(c *gin.Context, user *model.User) error {
	var mysqlErr *mysql.MySQLError
	err := ur.db.WithContext(c).Where("email = ? ", user.Email).Updates(user).Error
	if errors.As(err, &mysqlErr) && mysqlErr.Number == MysqlDuplicateError {
		return errors.New("唯一索引冲突")
	} else if err != nil {
		ur.log.Error("数据库异常", zap.Error(err))
		return errors.New("内部错误")
	}
	return nil
}
