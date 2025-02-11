package service

import (
	"SOJ/internal/entity"
	"SOJ/internal/repository"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"net/http"
	"time"
)

type SubmissionService struct {
	log    *zap.Logger
	repo   *repository.SubmissionRepository
	client *http.Client
	url    string
}

func NewSubmissionService(log *zap.Logger, repo *repository.SubmissionRepository) *SubmissionService {
	return &SubmissionService{
		log:    log,
		repo:   repo,
		client: &http.Client{Timeout: 10 * time.Second},
		url:    fmt.Sprintf("http://%s/submission", viper.GetString("judge0.addr")),
	}
}

func (ss *SubmissionService) Run(ctx *gin.Context, req *entity.Run) {

}
