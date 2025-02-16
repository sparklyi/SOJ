package service

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"SOJ/pkg/judge0"
	"SOJ/utils"
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
	"strconv"
)

type SubmissionService struct {
	log         *zap.Logger
	repo        *repository.SubmissionRepository
	problemRepo *repository.ProblemRepository
	langRepo    *repository.LanguageRepository
	userRepo    *repository.UserRepository
	judge       *judge0.Judge
}

func NewSubmissionService(log *zap.Logger, repo *repository.SubmissionRepository, p *repository.ProblemRepository, l *repository.LanguageRepository, j *judge0.Judge, u *repository.UserRepository) *SubmissionService {
	return &SubmissionService{
		log:         log,
		repo:        repo,
		problemRepo: p,
		langRepo:    l,
		judge:       j,
		userRepo:    u,
	}
}

// GetLimit 获取相关语言的时空限制
func (ss *SubmissionService) GetLimit(ctx *gin.Context, pid, lid int, oid string, s *model.Submission) (*entity.Limit, error) {
	//检查语言是否可用
	l, err := ss.langRepo.GetByID(ctx, lid)
	if err != nil {
		return nil, err
	} else if !(*l.Status) {
		return nil, errors.New(constant.DisableError)
	}

	//从mongo中得到当前测评语言的时空限制
	if oid == "" {
		p, terr := ss.problemRepo.GetInfoByID(ctx, pid)
		if terr != nil {
			return nil, terr
		}
		oid = p.ObjectID
	}
	objID, err := primitive.ObjectIDFromHex(oid)
	if err != nil {
		return nil, errors.New(constant.ParamError)
	}
	data, err := ss.problemRepo.GetInfoByObjID(ctx, objID)
	if err != nil {
		return nil, err
	}
	//正反序列化后得到时空限制
	bd, _ := bson.Marshal(data)
	t := entity.Problem{}
	err = bson.Unmarshal(bd, &t)
	if err != nil {
		return nil, err
	}

	//默认限制
	limit := entity.Limit{
		CpuTimeLimit:   2.0,        //s
		CpuMemoryLimit: 512 * 1024, //KB
	}
	//存在当前测评语言的限制
	if v, ok := t.LangLimit[strconv.Itoa(lid)]; ok {
		limit = v
	}
	//把部分信息存入
	if s != nil {
		s.ProblemName, s.Language = t.Name, l.Name
	}
	return &limit, nil
}

// Run 自测运行
func (ss *SubmissionService) Run(ctx *gin.Context, req *entity.Run) (*entity.JudgeResult, error) {

	limit, err := ss.GetLimit(ctx, req.ProblemID, req.LanguageID, req.ProblemObjID, nil)
	if err != nil {
		return nil, err
	}
	req.Limit = *limit
	req.CpuExtraLimit = req.CpuTimeLimit + 0.01
	r := ss.judge.Run(req)
	//数据清空, 自测不需要返回测评情况
	r.JudgeStatus = entity.JudgeStatus{}
	//go ss.repo.DeleteJudgeByToken(ctx, r.Token)
	return r, nil
}

// Judge 提交运行
func (ss *SubmissionService) Judge(ctx *gin.Context, req *entity.Run) (*model.Submission, error) {
	s := &model.Submission{}
	//获取当前测评语言的限制
	limit, err := ss.GetLimit(ctx, req.ProblemID, req.LanguageID, req.ProblemObjID, s)
	if err != nil {
		return nil, err
	}
	req.Limit = *limit
	req.CpuExtraLimit = req.CpuTimeLimit + 0.01
	//获取对应的测试点
	p, err := ss.problemRepo.GetInfoByID(ctx, req.ProblemID)
	if err != nil {
		return nil, err
	}
	if p.TestCaseID == "" {
		return nil, errors.New(constant.NotFoundError)
	}
	objID, _ := primitive.ObjectIDFromHex(p.TestCaseID)
	t, err := ss.problemRepo.GetTestCaseInfo(ctx, objID)
	if err != nil {
		return nil, err
	}

	//正反序列化得到测试点
	tc, _ := bson.Marshal(t)
	var testcase entity.TestCase
	err = bson.Unmarshal(tc, &testcase)
	if err != nil {
		return nil, err
	}

	//并发提交每个测试点
	n := len(testcase.Content)
	resp := make(chan entity.JudgeResult, n)
	defer close(resp)
	for _, v := range testcase.Content {
		req.Case = v
		go func(req entity.Run) {
			resp <- *ss.judge.Run(&req)
		}(*req)
	}
	claims := utils.GetAccessClaims(ctx)
	s.UserID = uint(claims.ID)
	s.ProblemID = uint(req.ProblemID)
	s.LanguageID = uint(req.LanguageID)
	s.ContestID = uint(req.ContestID)
	s.SourceCode = req.SourceCode

	//当contestId不为空时,从apply表获取用户名, 且记录不可见
	//反之从user表获取,记录可见
	if req.ContestID != 0 {
		s.Visible = new(bool)
		*s.Visible = false
		//TODO 查询apply表
	} else {
		//TODO 查询user表
		u, uErr := ss.userRepo.GetUserByID(ctx, claims.ID)
		if uErr != nil {
			return nil, uErr
		}
		s.UserName = u.Username
	}

	//测试点检查
	mxid := 0
	for range n {
		v := <-resp
		var jt float64
		if v.Time != "" {
			jt, _ = strconv.ParseFloat(v.Time, 64)
		}
		s.Time = max(s.Time, jt)
		s.Memory = max(s.Memory, v.Memory)
		if v.ID > mxid {
			mxid = v.ID
			s.Status, s.Stderr, s.CompileOut = v.Description, v.Stderr, v.CompileOutput
		}
		//未通过直接返回,后续测试点不再检查(ACM模式, 其他模式后续扩展)
		//if mxid != constant.JudgeAccepted {
		//	break
		//}
	}
	//测评机请求时间过长会导致上下文过长，gorm会警告慢sql
	err = ss.repo.CreateSubmission(ctx, s)
	if err != nil {
		return nil, err
	}
	return s, nil

}

// GetSubmissionInfoByID  根据ID获取测评详情
func (ss *SubmissionService) GetSubmissionInfoByID(ctx *gin.Context, id int) (*model.Submission, error) {
	return ss.repo.GetInfoByID(ctx, id)
}

// DeletePostgresJudgeHistory 删除postgres的测评历史记录
func (ss *SubmissionService) DeletePostgresJudgeHistory(ctx context.Context) error {
	return ss.repo.DeleteAllJudgeHistory(ctx)
}

// GetSubmissionList 获取测评列表
func (ss *SubmissionService) GetSubmissionList(ctx *gin.Context, req *entity.SubmissionList) ([]*model.Submission, error) {
	s, err := ss.repo.GetSubmissionList(ctx, req)
	for _, v := range s {
		v.SourceCode = ""
		v.Visible = nil
		v.Stderr, v.CompileOut = "", ""
	}
	return s, err
}

// GetSubmissionByID 根据测评id获取详情
func (ss *SubmissionService) GetSubmissionByID(ctx *gin.Context, id int) (*model.Submission, error) {
	s, err := ss.repo.GetInfoByID(ctx, id)
	if err != nil {
		return nil, err
	}
	claims := utils.GetAccessClaims(ctx)
	if *s.Visible == false && (claims.ID != int(s.UserID) || claims.Auth < constant.AdminLevel) {
		return nil, errors.New(constant.UnauthorizedError)
	}
	return s, nil
}
