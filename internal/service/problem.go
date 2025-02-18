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
	"sync"
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
func (ps *ProblemService) Create(ctx *gin.Context, req *entity.Problem) (*model.Problem, error) {
	//插入mongo

	pid, err := ps.repo.MongoCreate(ctx, req)
	if err != nil {
		return nil, err
	}
	problem := model.Problem{
		ObjectID: pid.Hex(),
		Name:     req.Name,
		Level:    req.Level,
		Status:   req.Visible,
		Owner:    req.Owner,
	}
	return &problem, ps.repo.MySQLCreate(ctx, &problem)
}

// Count 获取题目数量
func (ps *ProblemService) Count(ctx *gin.Context) (int64, error) {
	return ps.repo.Count(ctx)
}

// GetProblemList 获取题目列表
func (ps *ProblemService) GetProblemList(ctx *gin.Context, req *entity.ProblemList) ([]*model.Problem, error) {

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
	if claims.Auth != constant.RootLevel && (!*p.Status || *p.Owner != 0) {
		return nil, errors.New(constant.UnauthorizedError)
	}
	if p.ObjectID == "" {
		return nil, errors.New(constant.NotFoundError)
	}
	//转换为ObjectID对象
	obj, _ := primitive.ObjectIDFromHex(p.ObjectID)

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
	if p.ObjectID == "" {
		return errors.New(constant.NotFoundError)
	}
	objID, _ := primitive.ObjectIDFromHex(p.ObjectID)
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

	p, err := ps.repo.GetInfoByID(ctx, id)
	if err != nil {
		return err
	}
	if p.TestCaseID == "" || p.ObjectID == "" {
		return errors.New(constant.NotFoundError)
	}

	wg := sync.WaitGroup{}
	wg.Add(2)
	var perr error
	var terr error
	//删除题目文档
	go func() {
		defer wg.Done()
		objID, _ := primitive.ObjectIDFromHex(p.ObjectID)
		perr = ps.repo.MongoDeleteProblem(ctx, objID)
	}()
	//删除测试点文档
	go func() {
		defer wg.Done()
		objID, _ := primitive.ObjectIDFromHex(p.TestCaseID)
		terr = ps.repo.DeleteTestCase(ctx, objID)
	}()

	err = ps.repo.MysqlDeleteProblem(ctx, tx, id)
	if err != nil {
		tx.Rollback()
		return err
	}
	wg.Wait()
	if perr != nil || terr != nil {
		tx.Rollback()
		return errors.New(constant.ServerError)
	}
	tx.Commit()
	return nil

}

// GetTestCaseInfo 获取测试点信息
func (ps *ProblemService) GetTestCaseInfo(ctx *gin.Context, id int) (*bson.M, error) {
	//获取objID
	p, err := ps.repo.GetInfoByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.TestCaseID == "" {
		return nil, errors.New(constant.NotFoundError)
	}
	obj, _ := primitive.ObjectIDFromHex(p.TestCaseID)
	res, err := ps.repo.GetTestCaseInfo(ctx, obj)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CreateTestCase 创建测试点
func (ps *ProblemService) CreateTestCase(ctx *gin.Context, req *entity.TestCase, pid int) error {
	p, err := ps.repo.GetInfoByID(ctx, pid)
	if err != nil {
		return err
	}
	objID, err := ps.repo.CreateTestCase(ctx, req)
	if err != nil {
		return err
	}
	p.TestCaseID = objID.Hex()
	err = ps.repo.MysqlUpdateInfoByID(ctx, nil, p)
	if err != nil {
		return err
	}
	return nil
}

// UpdateTestCase 更新测试点
func (ps *ProblemService) UpdateTestCase(ctx *gin.Context, req *entity.TestCase, pid int) error {
	p, err := ps.repo.GetInfoByID(ctx, pid)
	if err != nil {
		return err
	}
	if p.TestCaseID == "" {
		return errors.New(constant.NotFoundError)
	}
	obj, _ := primitive.ObjectIDFromHex(p.TestCaseID)

	err = ps.repo.UpdateTestCase(ctx, req, obj)
	if err != nil {
		return err
	}
	return nil
}
