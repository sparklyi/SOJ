package model

import "gorm.io/gorm"

type Problem struct {
	gorm.Model
	ObjectID string `gorm:"varchar(30);unique;comment:对应存储在mongo的题目id" json:"object_id"`
	//查询字段
	Name   string `gorm:"varchar(255);not null" json:"name"`
	Level  string `gorm:"varchar(10);not null" json:"level"`
	Status *bool  `gorm:"default:false;comment:题目是否可见" json:"status"`
	Owner  *uint  `gorm:"index;default:0;comment:0表示公开题目, 其他情况为竞赛id" json:"owner"`
	//外键
	TestCase   []TestCase   `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"testCase,omitempty"`
	Submission []Submission `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"submission,omitempty"`
}

func (Problem) TableName() string {
	return "problem"
}
