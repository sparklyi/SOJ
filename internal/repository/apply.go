package repository

import (
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ApplyRepository struct {
	log *zap.Logger
	db  *gorm.DB
}

func NewApplyRepository(log *zap.Logger, db *gorm.DB) *ApplyRepository {
	return &ApplyRepository{
		log: log,
		db:  db,
	}
}
