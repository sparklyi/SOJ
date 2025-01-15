package model

import "gorm.io/gorm"

type Language struct {
	gorm.Model
	Name   string `gorm:"type:varchar(255);not null" json:"name"`
	MapID  uint   `gorm:"unique;comment:对应judge0的语言id" json:"map_id"`
	Status bool   `gorm:"default:true;comment:是否启用" json:"status"`
}

func (Language) TableName() string {
	return "language"
}
