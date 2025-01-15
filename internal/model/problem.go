package model

import "gorm.io/gorm"

type Problem struct {
	gorm.Model
	ObjectID string `gorm:"varchar(50);unique;comment:对应存储在mongo的题目id" json:"object_id"`
	//Source      string  `gorm:"type:text" json:"source"`
	//TimeLimit   float64 `gorm:"default:1;comment:限时" json:"time_limit"`
	//MemoryLimit float64 `gorm:"default:10240;comment:限存'" json:"memory_limit"`
	Status bool `gorm:"default:true" json:"status"`
}

func (Problem) TableName() string {
	return "problem"
}
