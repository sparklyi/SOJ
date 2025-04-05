package model

import "gorm.io/gorm"

type Problem struct {
	gorm.Model
	ObjectID   string `gorm:"type:varchar(30);unique;comment:对应存储在mongo的题目id" json:"object_id,omitemptytype:"`
	Name       string `gorm:"type:varchar(255);not null" json:"name,omitempty"`
	Level      string `gorm:"type:varchar(10);not null" json:"level,omitempty"`
	Status     *bool  `gorm:"default:false;comment:题目是否可见" json:"status,omitempty"`
	Owner      *uint  `gorm:"index;default:0;comment:0表示公开题目, 其他情况为竞赛id" json:"owner,omitempty"`
	TestCaseID string `gorm:"type:varchar(30);index;comment:对应存储在mongo中的测试点id" json:"test_case_id,omitempty"`
	UserID     uint   `gorm:"index;not null" json:"user_id,omitempty"`
	//外键

	Submission []Submission `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"submission,omitempty"`
}

func (Problem) TableName() string {
	return "problem"
}
