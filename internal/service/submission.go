package service

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/repository"
	"SOJ/pkg/judge0"
	"SOJ/utils"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"strconv"
	"time"
)

type SubmissionService interface {
	GetLimit(ctx *gin.Context, pid int, lid int, oid string, s *model.Submission) (*entity.Limit, error)
	Run(ctx *gin.Context, req *entity.Run) (*entity.JudgeResult, error)
	Judge(ctx *gin.Context, req *entity.Run) (*model.Submission, error)
	GetSubmissionInfoByID(ctx *gin.Context, id int) (*model.Submission, error)
	GetSubmissionList(ctx *gin.Context, req *entity.SubmissionList) ([]*model.Submission, error)
	GetSubmissionByID(ctx *gin.Context, id int) (*model.Submission, error)
}

type submission struct {
	log         *zap.Logger
	repo        repository.SubmissionRepository
	problemRepo repository.ProblemRepository
	langRepo    repository.LanguageRepository
	userRepo    repository.UserRepository
	applyRepo   repository.ApplyRepository
	contestRepo repository.ContestRepository
	judge       *judge0.Judge
}

func NewSubmissionService(log *zap.Logger, a repository.ApplyRepository, repo repository.SubmissionRepository, p repository.ProblemRepository, l repository.LanguageRepository, j *judge0.Judge, u repository.UserRepository, c repository.ContestRepository) SubmissionService {
	return &submission{
		log:         log,
		repo:        repo,
		problemRepo: p,
		langRepo:    l,
		judge:       j,
		userRepo:    u,
		applyRepo:   a,
		contestRepo: c,
	}
}

// GetLimit 获取相关语言的时空限制
func (ss *submission) GetLimit(ctx *gin.Context, pid, lid int, oid string, s *model.Submission) (*entity.Limit, error) {
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
	//序列化后得到时空限制
	t := entity.Problem{}
	err = utils.UnmarshalBSON(data, &t)

	if err != nil {
		return nil, err
	}

	//默认限制
	limit := entity.Limit{
		CpuTimeLimit:   constant.DefaultJudgeTimeLimit,
		CpuMemoryLimit: constant.DefaultJudgeMemoryLimit,
	}
	//存在当前测评语言的限制
	if v, ok := t.LangLimit[strconv.Itoa(lid)]; ok {
		limit = v
	}
	//把部分信息存入
	if s != nil {
		s.Language = l.Name
	}
	return &limit, nil
}

// Run 自测运行
func (ss *submission) Run(ctx *gin.Context, req *entity.Run) (*entity.JudgeResult, error) {

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
func (ss *submission) Judge(ctx *gin.Context, req *entity.Run) (*model.Submission, error) {
	s := &model.Submission{}
	var contestInfo *model.Contest

	if req.ContestID != 0 {
		//获取比赛信息
		var err error
		contestInfo, err = ss.contestRepo.GetContestInfoByID(ctx, req.ContestID)
		if err != nil {
			return nil, err
		}
		if time.Now().Unix() < contestInfo.StartTime.Unix() {
			return nil, errors.New("比赛未开始")
		}
	}

	//获取当前测评语言的限制
	limit, err := ss.GetLimit(ctx, req.ProblemID, req.LanguageID, req.ProblemObjID, s)
	if err != nil {
		return nil, err
	}
	req.Limit = *limit
	req.CpuExtraLimit = req.CpuTimeLimit + 0.01
	//获取对应的测试点存储id
	p, err := ss.problemRepo.GetInfoByID(ctx, req.ProblemID)
	if err != nil {
		return nil, err
	}
	if p.TestCaseID == "" {
		return nil, errors.New(constant.NotFoundError)
	}
	objID, _ := primitive.ObjectIDFromHex(p.TestCaseID)
	//获取mongo中的测试点详情
	t, err := ss.problemRepo.GetTestCaseInfo(ctx, objID)
	if err != nil {
		return nil, err
	}

	//序列化得到测试点
	var testcase entity.TestCase
	err = utils.UnmarshalBSON(t, &testcase)
	if err != nil {
		return nil, err
	}

	ss.log.Info("提交测评", zap.Any("request:", req))

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

	//构建测评记录
	claims := utils.GetAccessClaims(ctx)
	s.UserID = uint(claims.ID)
	s.ProblemID = uint(req.ProblemID)
	s.ProblemName = p.Name
	s.LanguageID = uint(req.LanguageID)
	s.ContestID = uint(req.ContestID)
	s.SourceCode = req.SourceCode

	//当contestId不为空时,从apply表获取用户名, 且记录不可见
	//反之从user表获取,记录可见
	var applyInfo *model.Apply

	if req.ContestID != 0 {
		s.Visible = new(bool)
		*s.Visible = false
		//查询apply表
		applyInfo, err = ss.applyRepo.GetInfoByUserAndContest(ctx, s.UserID, s.ContestID)
		if err != nil {
			return nil, err
		}
		s.UserName = applyInfo.Name

	} else {
		//查询user表
		u, uErr := ss.userRepo.GetUserByID(ctx, claims.ID)
		if uErr != nil {
			return nil, uErr
		}
		s.UserName = u.Username
	}

	//测试点检查
	statusID := 0

	for range n {
		v := <-resp
		var jt float64
		if v.Time != "" {
			jt, _ = strconv.ParseFloat(v.Time, 64)
		}
		s.Time = max(s.Time, jt)
		s.Memory = max(s.Memory, v.Memory)
		if v.ID > statusID {
			//状态更新
			statusID = v.ID
			s.Status, s.Stderr, s.CompileOut = v.Description, v.Stderr, v.CompileOutput
		}
		//未通过直接返回,后续测试点不再检查(ACM模式, 其他模式后续扩展) 提前关闭通道会导致panic
		//if statusID != constant.JudgeAC {
		//	break
		//}
	}

	var tx *gorm.DB
	//如果是比赛提交 解析出当前成绩
	if applyInfo != nil && contestInfo != nil && time.Now().Unix() <= contestInfo.EndTime.Unix() {
		var res entity.ContestScore
		//赛时第一次提交 没有个人成绩
		if applyInfo.Score == "" {
			res = entity.NewContestScore()
		} else {
			err = json.Unmarshal([]byte(applyInfo.Score), &res)
			if err != nil {
				return nil, errors.New(constant.JsonParseError)
			}
		}

		//实际的所有题目提交情况
		actual := res.Actual
		//冻结的所有题目提交情况
		freeze := res.Freeze
		//当前题目的提交记录
		record := actual.Details[s.ProblemID]
		record.Name = s.ProblemName
		//之前未通过本题
		if record.Status != constant.JudgeAC {
			record.Count++
			//本次提交通过测评
			if statusID == constant.JudgeAC {
				record.Status = constant.JudgeAC
				//更新总成绩
				actual.AcceptedCount++
				actual.PenaltyCount += record.Penalty + time.Since(*contestInfo.StartTime).Minutes()
				//res.ScoreCount IOI扩展

				//测评状态非系统错误
			} else if statusID > constant.JudgeAC && statusID < constant.JudgeIE {
				record.Status = constant.JudgeWA
				record.Penalty += constant.PenaltyTime
				//actual.Score IOI扩展
			}
			//本次测评结果保存
			actual.Details[s.ProblemID] = record
			//freeze.Details[s.ProblemID] = record

			//封榜 状态更新为冻结 记录提交次数 其他不更新
			if time.Now().Unix() >= contestInfo.FreezeTime.Unix() {
				tt := freeze.Details[s.ProblemID]
				//首次提交需要记录题目
				tt.Name = s.ProblemName
				tt.Status = constant.JudgeFreeze
				tt.Count = record.Count
				freeze.Details[s.ProblemID] = tt

				//未封榜 同步实际结果
			} else {
				freeze = actual
			}
			// 更新表数据
			res.Freeze = freeze
			res.Actual = actual
			//生成新的个人成绩json
			data, _ := json.Marshal(res)
			applyInfo.Score = string(data)

			//获取事务
			tx = ss.applyRepo.GetTransaction(ctx)
		}
	}
	if tx != nil {
		err = ss.applyRepo.UpdateApply(ctx, applyInfo, tx)
		if err != nil {
			return nil, err
		}
	}

	//测评机请求时间过长会导致上下文过长，gorm会警告慢sql
	err = ss.repo.CreateSubmission(ctx, s, tx)
	if err != nil {
		if tx != nil {
			tx.Rollback()
		}

		return nil, err
	}
	if tx != nil {
		tx.Commit()
	}

	return s, nil

}

// GetSubmissionInfoByID  根据ID获取测评详情
func (ss *submission) GetSubmissionInfoByID(ctx *gin.Context, id int) (*model.Submission, error) {
	return ss.repo.GetInfoByID(ctx, id)
}

// GetSubmissionList 获取测评列表
func (ss *submission) GetSubmissionList(ctx *gin.Context, req *entity.SubmissionList) ([]*model.Submission, error) {
	s, err := ss.repo.GetSubmissionList(ctx, req)
	for _, v := range s {
		v.SourceCode = ""
		v.Visible = nil
		v.Stderr, v.CompileOut = "", ""
	}
	return s, err
}

// GetSubmissionByID 根据测评id获取详情
func (ss *submission) GetSubmissionByID(ctx *gin.Context, id int) (*model.Submission, error) {
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
