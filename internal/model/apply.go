package model

import "gorm.io/gorm"

type Apply struct {
	gorm.Model
	UserID        uint   `gorm:"index;not null;comment:用户id" json:"user_id"`
	CompetitionID uint   `gorm:"index;not null;comment:竞赛id" json:"competition_id"`
	Name          string `gorm:"comment:比赛用户名称" json:"name"`
}

func (Apply) TableName() string {
	return "apply"
}
