package repository

import (
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type ProblemRepository struct {
	log   *zap.Logger
	db    *gorm.DB
	mongo *mongo.Database
}

func NewProblemRepository(log *zap.Logger, db *gorm.DB, m *mongo.Database) *ProblemRepository {
	return &ProblemRepository{
		log:   log,
		db:    db,
		mongo: m,
	}
}
