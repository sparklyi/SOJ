package model

import "gorm.io/gorm"

type Apply struct {
	gorm.Model
	UserID    uint   `gorm:"index;not null;comment:用户id" json:"user_id"`
	ContestID uint   `gorm:"index;not null;comment:竞赛id" json:"contest_id"`
	Name      string `gorm:"comment:比赛用户名称" json:"name"`
}

func (Apply) TableName() string {
	return "apply"
}
