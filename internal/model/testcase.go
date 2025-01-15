package model

import "gorm.io/gorm"

type TestCase struct {
	gorm.Model
	ProblemID uint   `gorm:"comment:题目id" json:"problem_id"`
	Input     string `gorm:"type:text;comment:标准输入" json:"input"`
	Output    string `gorm:"type:text;comment:标准输出" json:"output"`
}

func (TestCase) TableName() string {
	return "test_case"
}
