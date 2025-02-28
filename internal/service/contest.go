package service

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/model"
	"SOJ/internal/mq/producer"
	"SOJ/internal/repository"
	"SOJ/utils"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"sort"
	"time"
)

type ContestService interface {
	CreateContest(ctx *gin.Context, req *entity.Contest) (*model.Contest, error)
	UpdateContest(ctx *gin.Context, req *entity.Contest) error
	GetContestList(ctx *gin.Context, req *entity.ContestList) ([]*model.Contest, error)
	GetListByUserID(ctx *gin.Context, id int, page int, pageSize int) ([]*model.Contest, error)
	GetContestInfoByID(ctx *gin.Context, id int) (*model.Contest, error)
	DeleteContest(ctx *gin.Context, id int) error
	GetRankingList(ctx *gin.Context, req *entity.RankingList) ([]rank, error)
}

type contest struct {
	log       *zap.Logger
	repo      repository.ContestRepository
	applyRepo repository.ApplyRepository
	producer  *producer.Contest
}

// NewContestService 依赖注入
func NewContestService(logger *zap.Logger, repo repository.ContestRepository, a repository.ApplyRepository, p *producer.Contest) ContestService {
	return &contest{
		log:       logger,
		repo:      repo,
		applyRepo: a,
		producer:  p,
	}

}

// CreateContest 创建比赛
func (cs *contest) CreateContest(ctx *gin.Context, req *entity.Contest) (*model.Contest, error) {
	problemSet, _ := json.Marshal(req.ProblemSet)
	if req.Public == nil || *req.Public == false {
		req.Code = utils.GenerateRandCode(6, true)
	}
	c := &model.Contest{
		Name:        req.Name,
		UserID:      uint(utils.GetAccessClaims(ctx).ID),
		Tag:         req.Tag,
		Type:        req.Type,
		Sponsor:     req.Sponsor,
		Description: req.Description,
		ProblemSet:  string(problemSet),
		Public:      req.Public,
		Code:        req.Code,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
		FreezeTime:  req.FreezeTime,
		Publish:     req.Publish,
	}
	_, err := cs.repo.CreateContest(ctx, c)
	if err != nil {
		return nil, err
	}

	//距离开赛还有2天以上允许提前1天发送比赛提醒 且比赛已发布
	seconds := (*c.StartTime).Sub(time.Now()).Seconds()
	if seconds >= 48*60*60 && *c.Publish {
		//发布比赛邮件提醒
		data := producer.ContestNotify{
			ContestID: c.ID,
			Subject:   "比赛通知",
		}
		go cs.producer.Producer(ctx, &data, int64(seconds)-24*60*60)
	}
	//data := producer.ContestNotify{
	//	ContestID: c.ID,
	//	Subject:   "比赛通知",
	//	Content:   "test",
	//}
	//go cs.producer.Producer(ctx, &data, 20)
	return c, nil

}

// UpdateContest 更新比赛信息
func (cs *contest) UpdateContest(ctx *gin.Context, req *entity.Contest) error {
	c, err := cs.repo.GetContestInfoByID(ctx, req.ID)
	if err != nil {
		return err
	}
	claims := utils.GetAccessClaims(ctx)
	if c.UserID != uint(claims.ID) && claims.Auth < constant.AdminLevel {
		return errors.New(constant.UnauthorizedError)
	}
	j, _ := json.Marshal(req.ProblemSet)
	if req.Public != nil && *req.Public != *c.Public {
		if *req.Public == false {
			req.Code = utils.GenerateRandCode(6, true)
		}
	}
	data := map[string]interface{}{
		"name":        req.Name,
		"tag":         req.Tag,
		"description": req.Description,
		"publish":     req.Publish,
		"sponsor":     req.Sponsor,
		"start_time":  req.StartTime,
		"end_time":    req.EndTime,
		"freeze_time": req.FreezeTime,
		"problem_set": string(j),
		"public":      req.Public,
		"code":        req.Code,
	}

	return cs.repo.UpdateContest(ctx, data, c.ID)

}

// GetContestList 获取比赛列表
func (cs *contest) GetContestList(ctx *gin.Context, req *entity.ContestList) ([]*model.Contest, error) {
	claims := utils.GetAccessClaims(ctx)
	admin := claims != nil && claims.Auth > constant.UserLevel

	list, err := cs.repo.GetContestList(ctx, req, admin)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		v.Code = ""
		v.FreezeTime = nil
		v.Description = ""
		v.ProblemSet = ""
	}
	return list, nil
}

// GetListByUserID 获取用户比赛列表
func (cs *contest) GetListByUserID(ctx *gin.Context, id, page, pageSize int) ([]*model.Contest, error) {
	list, err := cs.repo.GetListByUserID(ctx, id, page, pageSize)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		v.ProblemSet = ""
		v.Description = ""
	}
	return list, nil
}

// GetContestInfoByID 获取比赛详情
func (cs *contest) GetContestInfoByID(ctx *gin.Context, id int) (*model.Contest, error) {
	c, err := cs.repo.GetContestInfoByID(ctx, id)
	if err != nil {
		return nil, err
	}
	claims := utils.GetAccessClaims(ctx)
	if claims.Auth < constant.AdminLevel && c.UserID != uint(claims.ID) {
		c.Code = ""
		c.ProblemSet = ""
	}
	return c, nil
}

// DeleteContest 删除比赛
func (cs *contest) DeleteContest(ctx *gin.Context, id int) error {
	c, err := cs.repo.GetContestInfoByID(ctx, id)
	if err != nil {
		return err
	}
	claims := utils.GetAccessClaims(ctx)
	if claims.Auth < constant.AdminLevel && c.UserID != uint(claims.ID) {
		return errors.New(constant.UnauthorizedError)
	}
	err = cs.repo.DeleteContest(ctx, id)
	if err != nil {
		return err
	}
	go cs.applyRepo.DeleteApplyByContestID(ctx, id)
	return nil
}

type rank struct {
	ApplyName string              `json:"apply_name"`
	Info      entity.ContestScore `json:"info"`
}

// GetRankingList 获取排行榜
func (cs *contest) GetRankingList(ctx *gin.Context, req *entity.RankingList) ([]rank, error) {
	list, err := cs.applyRepo.GetListByContestID(ctx, req.ContestID, 1, 500)
	if err != nil {
		return nil, err
	}
	c, err := cs.repo.GetContestInfoByID(ctx, req.ContestID)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	//管理员权限可查看实时榜单
	show := utils.GetAccessClaims(ctx).Auth > constant.UserLevel
	//比赛未到封榜时间 或 比赛结束一天后 显示实际成绩
	if now < c.FreezeTime.Unix() || now >= c.EndTime.Add(time.Hour*constant.ReliveTime).Unix() {
		show = true
	}

	score := make([]rank, len(list))
	for i, v := range list {
		score[i].ApplyName = v.Name
		//有成绩则解析
		if v.Score != "" {
			err = json.Unmarshal([]byte(v.Score), &score[i].Info)
			if err != nil {
				cs.log.Error("json 解析失败", zap.Error(err), zap.Any("score", v.Score))
				return nil, errors.New(constant.ServerError)
			}
			if show {
				score[i].Info.Freeze = score[i].Info.Actual
			}
			score[i].Info.Actual = entity.ProblemSetResult{}
		}

	}
	// ACM模式排序
	sort.Slice(score, func(i, j int) bool {
		a, b := score[i].Info.Freeze, score[j].Info.Freeze
		if a.AcceptedCount == b.AcceptedCount {
			return a.PenaltyCount < b.PenaltyCount
		}
		return a.AcceptedCount > b.AcceptedCount
	})
	//部分信息清空

	return score, nil

}
