package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"errors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ProblemRepository struct {
	log   *zap.Logger
	db    *gorm.DB
	mongo *mongo.Collection
}

func NewProblemRepository(log *zap.Logger, db *gorm.DB, m *mongo.Database) *ProblemRepository {
	return &ProblemRepository{
		log:   log,
		db:    db,
		mongo: m.Collection("problem"),
	}
}

// MongoCreate 创建mongo记录
func (pr *ProblemRepository) MongoCreate(ctx *gin.Context, req *entity.Problem) (primitive.ObjectID, error) {
	res, err := pr.mongo.InsertOne(ctx, req)
	if err != nil {
		pr.log.Error("mongo写入失败", zap.Error(err))
		return primitive.ObjectID{}, errors.New(constant.ServerError)
	}
	return res.InsertedID.(primitive.ObjectID), nil

}

// MySQLCreate 创建mysql记录
func (pr *ProblemRepository) MySQLCreate(ctx *gin.Context, problem *model.Problem) error {
	err := pr.db.WithContext(ctx).Create(problem).Error
	if err != nil {
		pr.log.Error("数据库插入失败", zap.Error(err))
		return errors.New(constant.ServerError)

	}
	return nil
}

// Count 题目数量
func (pr *ProblemRepository) Count(ctx *gin.Context) (int64, error) {
	var total int64
	if err := pr.db.WithContext(ctx).Model(&model.Problem{}).Count(&total).Error; err != nil {
		pr.log.Error("数据库查询失败", zap.Error(err))
		return -1, errors.New(constant.ServerError)
	}
	return total, nil
}

// GetProblemList 获取题目列表(条件筛选+分页)
func (pr *ProblemRepository) GetProblemList(ctx *gin.Context, req *entity.ProblemList, admin bool) (*[]model.Problem, error) {
	var sets []model.Problem
	db := pr.db.WithContext(ctx).Model(&model.Problem{})
	if req.ID != 0 {
		db = db.Where("id = ?", req.ID)
	}
	if req.Name != "" {
		db = db.Where("name LIKE ", req.Name+"%")
	}
	if req.Level != "" {
		db = db.Where("level = ?", req.Level)
	}
	//管理员可查看所有类型题目 普通用户只可查看公开题目
	if !admin {
		db = db.Where("status = true adn owner = 0")
	}
	err := db.Scopes(utils.Paginate(req.Page, req.PageSize)).Find(&sets).Error
	if err != nil {
		pr.log.Error("数据库查询失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return &sets, nil
}

// GetInfoByID 根据ID获取数据库信息
func (pr *ProblemRepository) GetInfoByID(ctx *gin.Context, id int) (*model.Problem, error) {
	p := model.Problem{Model: gorm.Model{ID: uint(id)}}
	if err := pr.db.WithContext(ctx).First(&p).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(constant.NotFoundError)
	} else if err != nil {
		pr.log.Error("数据库查询失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return &p, nil

}

// GetInfoByObjID 通过对象id获取对应的文档内容
func (pr *ProblemRepository) GetInfoByObjID(ctx *gin.Context, obj primitive.ObjectID) (*bson.M, error) {
	var res bson.M
	err := pr.mongo.FindOne(ctx, bson.M{"_id": obj}).Decode(&res)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errors.New(constant.NotFoundError)
		}
		pr.log.Error("mongo查询失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}

	return &res, nil
}
