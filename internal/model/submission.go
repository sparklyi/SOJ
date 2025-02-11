package model

import "gorm.io/gorm"

type Submission struct {
	gorm.Model
	UserID        uint    `json:"user_id"`
	ProblemID     uint    `json:"problem_id"`
	LanguageID    uint    `json:"language_id"`
	CompetitionID uint    `gorm:"default:0;0表示非比赛提交" json:"competition_id"`
	SourceCode    string  `gorm:"type:longtext;comment:源代码'" json:"source_code"`
	Status        string  `gorm:"type:varchar(255);comment:测评状态'" json:"status"`
	Time          float64 `gorm:"comment:测评耗时" json:"time"`
	Memory        float64 `gorm:"comment:测评耗存" json:"memory"`
	Stderr        string  `gorm:"type:longtext;comment:标准错误'" json:"stderr"`
	CompileOut    string  `gorm:"type:longtext;comment:编译输出'" json:"compile_out"`
	Token         string  `gorm:"type:varchar(100);" json:"token"`
	Visible       *bool   `gorm:"default:true;comment:是否可见" json:"visible"`
}

func (Submission) TableName() string {
	return "submission"
}
