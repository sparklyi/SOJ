package repository

import (
	"SOJ/internal/model"
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

func NewUserRepository(log *zap.Logger, db *gorm.DB, rs *redis.Client) *UserRepository {
	return &UserRepository{
		log: log,
		db:  db,
		rs:  rs,
	}
}

func (ur *UserRepository) Register(c *gin.Context, user *model.User) error {
	err := ur.db.Create(user).Error
	var mysqlErr *mysql.MySQLError

	if errors.As(err, &mysqlErr) && mysqlErr.Number == MysqlDuplicateError {
		return errors.New("该邮箱已注册")
	}
	if err != nil {
		return errors.New("服务器异常")
	}
	return nil

}
