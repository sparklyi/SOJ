package model

import "gorm.io/gorm"

type Problem struct {
	gorm.Model
	ObjectID string `gorm:"varchar(30);unique;comment:对应存储在mongo的题目id" json:"object_id,omitempty"`
	//查询字段
	Name       string `gorm:"varchar(255);not null" json:"name,omitempty"`
	Level      string `gorm:"varchar(10);not null" json:"level,omitempty"`
	Status     *bool  `gorm:"default:false;comment:题目是否可见" json:"status,omitempty"`
	Owner      *uint  `gorm:"index;default:0;comment:0表示公开题目, 其他情况为竞赛id" json:"owner,omitempty"`
	TestCaseID string `gorm:"varchar(30);index;comment:对应存储在mongo中的测试点id" json:"test_case_id,omitempty"`
	//外键

	Submission []Submission `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"submission,omitempty"`
}

func (Problem) TableName() string {
	return "problem"
}
