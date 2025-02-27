package model

import "gorm.io/gorm"

type Apply struct {
	gorm.Model
	UserID    uint   `gorm:"index;not null;comment:用户id" json:"user_id"`
	ContestID uint   `gorm:"index;not null;comment:竞赛id" json:"contest_id"`
	Name      string `gorm:"type:varchar(255);comment:比赛用户名称" json:"name"`
	Email     string `gorm:"type:varchar(255);not null;comment:邮箱" json:"email,omitempty"`
	Score     string `gorm:"type:json;unique;comment:比赛成绩" json:"object_id"`
}

func (Apply) TableName() string {
	return "apply"
}
