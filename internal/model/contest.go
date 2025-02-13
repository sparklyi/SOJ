package model

import "gorm.io/gorm"

type Contest struct {
	gorm.Model
	//存到mongo
	ObjectID string `gorm:"varchar(30);unique;comment:对应存储在mongo的竞赛id" json:"object_id"`

	//外键
	Apply []Apply `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"apply,omitempty"`
}

func (Contest) TableName() string {
	return "contest"
}
