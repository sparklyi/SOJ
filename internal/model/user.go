package model

import "gorm.io/gorm"

type User struct {
	gorm.Model
	Username string `gorm:"type:varchar(255);not null;comment:用户名" json:"username,omitempty"`
	Password string `gorm:"type:varchar(255);not null;comment:密码" json:"password,omitempty"`
	Email    string `gorm:"type:varchar(255);not null;unique;comment:邮箱" json:"email,omitempty"`
	Avatar   string `gorm:"type:varchar(512);not null;comment:头像链接" json:"avatar,omitempty"`
	Role     int    `gorm:"type:int;not null;default:1;comment:-1封禁 1用户 2管理员 3超级管理员" json:"role,omitempty"`

	//外键
	Submission []Submission `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"submission,omitempty"`
	Apply      []Apply      `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"apply,omitempty"`
}

func (User) TableName() string {
	return "user"
}
