package service

import (
	"SOJ/internal/constant"
	"SOJ/internal/entity"
	"SOJ/internal/repository"
	"SOJ/pkg/judge0"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
	"net/http"
	"strconv"
	"time"
)

type SubmissionService struct {
	log         *zap.Logger
	repo        *repository.SubmissionRepository
	problemRepo *repository.ProblemRepository
	client      *http.Client
	JudgeUrl    string
	//judge       *mq.JudgeProducer
}

func NewSubmissionService(log *zap.Logger, repo *repository.SubmissionRepository, p *repository.ProblemRepository) *SubmissionService {
	return &SubmissionService{
		log:         log,
		repo:        repo,
		problemRepo: p,
		client:      &http.Client{Timeout: 10 * time.Second},
		JudgeUrl:    fmt.Sprintf("http://%s/submissions/?wait=true", viper.GetString("judge0.addr")),
	}
}

// GetLimit 获取相关语言的时空限制
func (ss *SubmissionService) GetLimit(ctx *gin.Context, pid, lid int, oid string) (*entity.Limit, error) {
	//从mongo中得到当前测评语言的时空限制
	if oid == "" {
		p, err := ss.problemRepo.GetInfoByID(ctx, pid)
		if err != nil {
			return nil, err
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
		TimeLimit:   1.0,        //s
		MemoryLimit: 256 * 1024, //KB
	}
	//存在当前测评语言的限制
	if v, ok := t.LangLimit[strconv.Itoa(lid)]; ok {
		limit = v
	}
	return &limit, nil
}

func (ss *SubmissionService) Run(ctx *gin.Context, req *entity.Run) (*entity.JudgeResult, error) {

	limit, err := ss.GetLimit(ctx, req.ProblemID, req.LanguageID, req.ProblemObjID)
	if err != nil {
		return nil, err
	}
	req.Limit = *limit
	req.CpuExtraLimit = req.TimeLimit + 0.01
	r, err := judge0.Run(ss.client, req, ss.JudgeUrl)
	if err != nil {
		return nil, err
	}
	return r, nil
}
