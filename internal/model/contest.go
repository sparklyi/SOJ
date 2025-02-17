package model

import (
	"gorm.io/gorm"
	"time"
)

type Contest struct {
	gorm.Model
	Name        string     `gorm:"type:varchar(255);not null;comment:竞赛名" json:"name,omitempty"`
	UserID      uint       `json:"user_id,omitempty"`
	Tag         int        `gorm:"comment:竞赛级别" json:"tag,omitempty"`
	Type        int        `gorm:"comment:竞赛模式" json:"type,omitempty"`
	Sponsor     string     `gorm:"comment:主办方" json:"sponsor,omitempty"`
	Description string     `gorm:"type:text;comment:比赛简介" json:"description,omitempty"`
	ProblemSet  string     `gorm:"type:json;comment:题目集合" json:"problem_set,omitempty"`
	Public      *bool      `gorm:"default:true;comment:是否公开比赛" json:"public,omitempty"`
	Code        string     `gorm:"comment:私人比赛邀请码" json:"code,omitempty"`
	StartTime   *time.Time `json:"start_time,omitempty"`
	EndTime     *time.Time `json:"end_time,omitempty"`
	FreezeTime  *time.Time `json:"freeze_time,omitempty"`

	//外键
	Apply []Apply `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"apply,omitempty" `
}

func (Contest) TableName() string {
	return "contest"
}

/*
	{
	}
*/
