package service

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"SOJ/utils"
	"errors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

type ProblemService struct {
	log  *zap.Logger
	repo *repository.ProblemRepository
}

func NewProblemService(log *zap.Logger, r *repository.ProblemRepository) *ProblemService {
	return &ProblemService{
		log:  log,
		repo: r,
	}
}

// Create 题目创建
func (ps *ProblemService) Create(ctx *gin.Context, req *entity.Problem) error {
	//插入mongo
	pid, err := ps.repo.MongoCreate(ctx, req)
	if err != nil {
		return err
	}
	problem := model.Problem{
		ObjectID: pid.Hex(),
		Name:     req.Name,
		Level:    req.Level,
		Status:   req.Visible,
		Owner:    req.Owner,
	}
	return ps.repo.MySQLCreate(ctx, &problem)
}

// Count 获取题目数量
func (ps *ProblemService) Count(ctx *gin.Context) (int64, error) {
	return ps.repo.Count(ctx)
}

// GetProblemList 获取题目列表
func (ps *ProblemService) GetProblemList(ctx *gin.Context, req *entity.ProblemList) (*[]model.Problem, error) {

	return ps.repo.GetProblemList(ctx, req, utils.GetAccessClaims(ctx).Auth == constant.RootLevel)
}

// GetProblemInfo 获取题目详情
func (ps *ProblemService) GetProblemInfo(ctx *gin.Context, id int) (*bson.M, error) {
	p, err := ps.repo.GetInfoByID(ctx, id)
	if err != nil {
		return nil, err
	}
	claims := utils.GetAccessClaims(ctx)
	//有鉴权中间件则不可能为空
	//非管理员 且 (题目不可见 或 题目未公开)
	if claims.Auth != constant.RootLevel && (!p.Status || p.Owner != 0) {
		return nil, errors.New(constant.UnauthorizedError)
	}
	//转换为ObjectID对象
	obj, err := primitive.ObjectIDFromHex(p.ObjectID)
	if err != nil {
		ps.log.Error("objectID转换HEX失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	//获取文档内容
	res, err := ps.repo.GetInfoByObjID(ctx, obj)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// UpdateProblemInfo 更新题目内容
func (ps *ProblemService) UpdateProblemInfo(ctx *gin.Context, req *entity.Problem) error {
	tx := ps.repo.GetTransaction(ctx)

	p, err := ps.repo.GetInfoByID(ctx, int(req.ID))
	if err != nil {
		return err
	}

	// 并行
	mongoErrChan := make(chan error, 1)

	objID, err := primitive.ObjectIDFromHex(p.ObjectID)
	if err != nil {
		ps.log.Error("object id转换失败", zap.Error(err))
		return errors.New(constant.ServerError)
	}
	// 启动Mongo更新协程
	go func() {
		mongoErrChan <- ps.repo.MongoUpdateInfoByObjID(ctx, req, objID)
	}()

	p.Level = req.Level
	p.Owner = req.Owner
	p.Name = req.Name
	p.Status = req.Visible
	if err = ps.repo.MysqlUpdateInfoByID(ctx, tx, p); err != nil {
		tx.Rollback()
		//后续引入补偿机制
		return err
	}

	// 等待Mongo结果
	if mongoErr := <-mongoErrChan; mongoErr != nil {
		tx.Rollback()
		return mongoErr
	}

	tx.Commit()
	return nil
}

// DeleteProblem 题目删除
func (ps *ProblemService) DeleteProblem(ctx *gin.Context, id int) error {
	tx := ps.repo.GetTransaction(ctx)

	ok := make(chan error, 1)
	go func() {
		p, err := ps.repo.GetInfoByID(ctx, id)
		if err != nil {
			ok <- err
		}
		objID, err := primitive.ObjectIDFromHex(p.ObjectID)
		if err != nil {
			ok <- err
		}
		ok <- ps.repo.MongoDeleteProblem(ctx, objID)
	}()

	err := ps.repo.MysqlDeleteProblem(ctx, tx, id)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err = <-ok; err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil

}
