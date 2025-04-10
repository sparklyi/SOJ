package service

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"SOJ/utils"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

type ProblemService interface {
	Create(ctx *gin.Context, req *entity.Problem) (*model.Problem, error)
	JudgeStatusCount(ctx *gin.Context, id int) (map[string]string, error)
	GetProblemList(ctx *gin.Context, req *entity.ProblemList) ([]*model.Problem, int64, error)
	GetProblemInfo(ctx *gin.Context, id int) (*bson.M, error)
	UpdateProblemInfo(ctx *gin.Context, req *entity.Problem) error
	DeleteProblem(ctx *gin.Context, id int) error
	GetTestCaseInfo(ctx *gin.Context, id int) (*bson.M, error)
	CreateTestCase(ctx *gin.Context, req *entity.TestCase, pid int) error
	UpdateTestCase(ctx *gin.Context, req *entity.TestCase, pid int) error
}

type problem struct {
	log  *zap.Logger
	repo repository.ProblemRepository
	//retry *producer.Retry
	rs *redis.Client
}

func NewProblemService(log *zap.Logger, r repository.ProblemRepository, rs *redis.Client) ProblemService {
	return &problem{
		log:  log,
		repo: r,
		rs:   rs,
	}
}

// Create 题目创建
func (ps *problem) Create(ctx *gin.Context, req *entity.Problem) (*model.Problem, error) {
	//插入mongo
	pid, err := ps.repo.MongoCreate(ctx, req)
	if err != nil {
		return nil, err
	}
	//题目创建默认不可见 必须添加测试点
	p := model.Problem{
		ObjectID: pid.Hex(),
		Name:     req.Name,
		Level:    req.Level,
		Owner:    req.Owner,
		UserID:   uint(utils.GetAccessClaims(ctx).ID),
	}
	return &p, ps.repo.MySQLCreate(ctx, &p)
}

// JudgeStatusCount 题目测评数量统计
func (ps *problem) JudgeStatusCount(ctx *gin.Context, id int) (map[string]string, error) {
	countName := fmt.Sprintf("%s-%v", constant.ProblemCount, id)
	res, err := ps.rs.HGetAll(ctx, countName).Result()
	if err != nil {
		ps.log.Error("获取题目测评数据失败", zap.Error(err))
		return nil, errors.New(constant.ServerError)
	}
	return res, nil
}

// GetProblemList 获取题目列表
func (ps *problem) GetProblemList(ctx *gin.Context, req *entity.ProblemList) ([]*model.Problem, int64, error) {
	claims := utils.GetAccessClaims(ctx)
	if claims != nil && req.UserID != 0 && claims.Auth < constant.RootLevel {
		req.UserID = claims.ID
	}
	return ps.repo.GetProblemList(ctx, req, claims != nil && claims.Auth == constant.RootLevel)
}

// GetProblemInfo 获取题目详情
func (ps *problem) GetProblemInfo(ctx *gin.Context, id int) (*bson.M, error) {
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
func (ps *problem) UpdateProblemInfo(ctx *gin.Context, req *entity.Problem) error {
	tx := ps.repo.GetTransaction(ctx)

	p, err := ps.repo.GetInfoByID(ctx, int(req.ID))
	if err != nil {
		return err
	}
	// 并行
	//mongoErrChan := make(chan error, 1)
	if p.ObjectID == "" {
		return errors.New(constant.NotFoundError)
	}
	//公开题目时必须存在测试点
	if *p.Status == false && req.Visible != nil && *req.Visible == true && p.TestCaseID == "" {
		return errors.New(constant.DisableError)
	}
	objID, _ := primitive.ObjectIDFromHex(p.ObjectID)
	//// 启动Mongo更新协程
	//go func() {
	//	mongoErrChan <- ps.repo.MongoUpdateInfoByObjID(ctx, req, objID)
	//}()

	p.Level = req.Level
	p.Owner = req.Owner
	p.Name = req.Name
	p.Status = req.Visible
	if err = ps.repo.MysqlUpdateInfoByID(ctx, tx, p); err != nil {
		tx.Rollback()
		//重试补偿
		//j, _ := json.Marshal(*req)
		//data := producer.RetryContent{
		//	FuncName: "MongoUpdateInfoByObjID",
		//	ObjectID: objID,
		//	Params:   string(j),
		//}
		//go ps.retry.Send(ctx, data)
		return err
	}

	// 等待Mongo结果
	if err = ps.repo.MongoUpdateInfoByObjID(ctx, req, objID); err != nil {
		tx.Rollback()
		return err
	}

	tx.Commit()
	return nil
}

// DeleteProblem 题目删除
func (ps *problem) DeleteProblem(ctx *gin.Context, id int) error {
	//tx := ps.repo.GetTransaction(ctx)
	//题目删除后 不需要关注mongo文档删除成功, id是访问不了的, 后续定时扫描删除即可
	p, err := ps.repo.GetInfoByID(ctx, id)
	if err != nil {
		return err
	}
	//if p.TestCaseID == "" || p.ObjectID == "" {
	//	return errors.New(constant.NotFoundError)
	//}
	claims := utils.GetAccessClaims(ctx)
	if claims.Auth != constant.RootLevel && p.UserID != uint(claims.ID) {
		return errors.New(constant.UnauthorizedError)

	}
	err = ps.repo.MysqlDeleteProblem(ctx, nil, id)
	if err != nil {
		return err
	}

	//删除题目文档
	go func() {
		objID, _ := primitive.ObjectIDFromHex(p.ObjectID)
		ps.repo.MongoDeleteProblem(ctx, objID)
	}()
	//删除测试点文档
	go func() {
		objID, _ := primitive.ObjectIDFromHex(p.TestCaseID)
		ps.repo.DeleteTestCase(ctx, objID)
	}()

	return nil

}

// GetTestCaseInfo 获取测试点信息
func (ps *problem) GetTestCaseInfo(ctx *gin.Context, id int) (*bson.M, error) {
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
func (ps *problem) CreateTestCase(ctx *gin.Context, req *entity.TestCase, pid int) error {
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
func (ps *problem) UpdateTestCase(ctx *gin.Context, req *entity.TestCase, pid int) error {
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
