package model

import "gorm.io/gorm"

type Competition struct {
	gorm.Model
	Name string `gorm:"type:varchar(255);unique;comment:竞赛名" json:"name"`
}
