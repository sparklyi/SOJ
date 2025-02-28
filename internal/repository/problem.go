package repository

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/utils"
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ProblemRepository interface {
	GetTransaction(ctx context.Context) *gorm.DB
	MongoCreate(ctx context.Context, req *entity.Problem) (primitive.ObjectID, error)
	MySQLCreate(ctx context.Context, problem *model.Problem) error
	Count(ctx context.Context, admin bool) (int64, error)
	GetProblemList(ctx context.Context, req *entity.ProblemList, admin bool) ([]*model.Problem, error)
	GetInfoByID(ctx context.Context, id int) (*model.Problem, error)
	GetInfoByObjID(ctx context.Context, obj primitive.ObjectID) (*bson.M, error)
	MysqlUpdateInfoByID(ctx context.Context, tx *gorm.DB, problem *model.Problem) error
	MongoUpdateInfoByObjID(ctx context.Context, req *entity.Problem, objID primitive.ObjectID) error
	MysqlDeleteProblem(ctx context.Context, tx *gorm.DB, id int) error
	MongoDeleteProblem(ctx context.Context, objID primitive.ObjectID) error
	GetTestCaseInfo(ctx context.Context, objID primitive.ObjectID) (*bson.M, error)
	CreateTestCase(ctx context.Context, req *entity.TestCase) (primitive.ObjectID, error)
	UpdateTestCase(ctx context.Context, req *entity.TestCase, objID primitive.ObjectID) error
	DeleteTestCase(ctx context.Context, objID primitive.ObjectID) error
}

type problem struct {
	log          *zap.Logger
	db           *gorm.DB
	ProblemColl  *mongo.Collection
	TestCaseColl *mongo.Collection
}

func NewProblemRepository(log *zap.Logger, db *gorm.DB, m *mongo.Database) ProblemRepository {
	return &problem{
		log:          log,
		db:           db,
		ProblemColl:  m.Collection("problem"),
		TestCaseColl: m.Collection("test_case"),
	}
}

func (pr *problem) GetTransaction(ctx context.Context) *gorm.DB {
	return pr.db.WithContext(ctx).Begin()
}

// MongoCreate 创建mongo记录
func (pr *problem) MongoCreate(ctx context.Context, req *entity.Problem) (primitive.ObjectID, error) {
	res, err := pr.ProblemColl.InsertOne(ctx, req)
	if err != nil {
		pr.log.Error("mongo写入失败", zap.Error(err))
		return primitive.ObjectID{}, errors.New(constant.ServerError)
	}
	return res.InsertedID.(primitive.ObjectID), nil

}

// MySQLCreate 创建mysql记录
func (pr *problem) MySQLCreate(ctx context.Context, problem *model.Problem) error {
	err := pr.db.WithContext(ctx).Create(problem).Error
	if err != nil {
		pr.log.Error("数据库插入失败", zap.Error(err))
		return errors.New(constant.ServerError)

	}
	return nil
}

// Count 题目数量
func (pr *problem) Count(ctx context.Context, admin bool) (int64, error) {
	var total int64
	db := pr.db.WithContext(ctx).Model(&model.Problem{})
	if !admin {
		db = db.Where("status = true and owner=0")
	}
	if err := db.Count(&total).Error; err != nil {
		pr.log.Error("数据库查询失败", zap.Error(err))
		return -1, errors.New(constant.ServerError)
	}
	return total, nil
}

// GetProblemList 获取题目列表(条件筛选+分页)
func (pr *problem) GetProblemList(ctx context.Context, req *entity.ProblemList, admin bool) ([]*model.Problem, error) {
	var sets []*model.Problem
	db := pr.db.WithContext(ctx).Model(&model.Problem{})
	if req.ID != 0 {
		db = db.Where("id = ?", req.ID)
	}
	if req.Name != "" {
		db = db.Where("name LIKE ", "%"+req.Name+"%")
	}
	if req.Level != "" {
		db = db.Where("level = ?", req.Level)
	}
	//管理员可查看所有类型题目 普通用户只可查看公开题目
	if !admin {
		db = db.Where("status = true and owner = 0")
	}
	err := db.Scopes(utils.Paginate(req.Page, req.PageSize)).Find(&sets).Error
	if err != nil {
		pr.log.Error("数据库查询失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return sets, nil
}

// GetInfoByID 根据ID获取数据库信息
func (pr *problem) GetInfoByID(ctx context.Context, id int) (*model.Problem, error) {
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
func (pr *problem) GetInfoByObjID(ctx context.Context, obj primitive.ObjectID) (*bson.M, error) {
	var res bson.M
	err := pr.ProblemColl.FindOne(ctx, bson.M{"_id": obj}).Decode(&res)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errors.New(constant.NotFoundError)
		}
		pr.log.Error("mongo查询失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}

	return &res, nil
}

// MysqlUpdateInfoByID 题目信息更新
func (pr *problem) MysqlUpdateInfoByID(ctx context.Context, tx *gorm.DB, problem *model.Problem) error {
	if tx == nil {
		tx = pr.db.WithContext(ctx)
	}
	err := tx.Save(problem).Error
	if err != nil {
		pr.log.Error("数据库更新失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// MongoUpdateInfoByObjID mongo中的信息更新
func (pr *problem) MongoUpdateInfoByObjID(ctx context.Context, req *entity.Problem, objID primitive.ObjectID) error {
	filter := bson.M{"_id": objID}
	update := bson.M{"$set": req}
	res, err := pr.ProblemColl.UpdateOne(ctx, filter, update)
	if err != nil {
		pr.log.Error("mongo文档更新失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	if res.MatchedCount == 0 {
		return errors.New(constant.NotFoundError)
	}
	return nil
}

// MysqlDeleteProblem mysql中的题目删除
func (pr *problem) MysqlDeleteProblem(ctx context.Context, tx *gorm.DB, id int) error {
	if tx == nil {
		tx = pr.db.WithContext(ctx)
	}
	err := tx.Where("id = ?", id).Delete(&model.Problem{}).Error
	if err != nil {
		pr.log.Error("数据库删除记录失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}

// MongoDeleteProblem mongo中的题目删除
func (pr *problem) MongoDeleteProblem(ctx context.Context, objID primitive.ObjectID) error {
	filter := bson.M{"_id": objID}
	_, err := pr.ProblemColl.DeleteOne(ctx, filter)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		pr.log.Error("mongo删除文档失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	//if res.DeletedCount == 0 {
	//	return errors.New(constant.NotFoundError)
	//}
	return nil
}

// GetTestCaseInfo 获取测试点
func (pr *problem) GetTestCaseInfo(ctx context.Context, objID primitive.ObjectID) (*bson.M, error) {

	var res *bson.M
	err := pr.TestCaseColl.FindOne(ctx, bson.M{"_id": objID}).Decode(&res)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errors.New(constant.NotFoundError)
		}
		pr.log.Error("获取mongo文档失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return res, nil
}

// CreateTestCase 创建测试点
func (pr *problem) CreateTestCase(ctx context.Context, req *entity.TestCase) (primitive.ObjectID, error) {
	res, err := pr.TestCaseColl.InsertOne(ctx, req)
	if err != nil {
		pr.log.Error("测试点写入失败", zap.Error(err))
		return primitive.ObjectID{}, errors.New(constant.ServerError)
	}
	return res.InsertedID.(primitive.ObjectID), nil

}

// UpdateTestCase 更新测试点
func (pr *problem) UpdateTestCase(ctx context.Context, req *entity.TestCase, objID primitive.ObjectID) error {
	filter := bson.M{"_id": objID}
	update := bson.M{"$set": req}
	res, err := pr.TestCaseColl.UpdateOne(ctx, filter, update)
	if err != nil {
		pr.log.Error("测试点更新失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	if res.MatchedCount == 0 {
		return errors.New(constant.NotFoundError)
	}
	return nil
}

// DeleteTestCase 删除测试点
func (pr *problem) DeleteTestCase(ctx context.Context, objID primitive.ObjectID) error {
	filter := bson.M{"_id": objID}
	_, err := pr.TestCaseColl.DeleteOne(ctx, filter)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		pr.log.Error("mongo删除文档失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	return nil
}
