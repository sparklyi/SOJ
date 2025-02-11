package repository

import (
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type SubmissionRepository struct {
	log *zap.Logger
	db  *gorm.DB
}

func NewSubmissionRepository(log *zap.Logger, db *gorm.DB) *SubmissionRepository {
	return &SubmissionRepository{
		log: log,
		db:  db,
	}
}
