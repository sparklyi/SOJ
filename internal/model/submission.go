package model

import "gorm.io/gorm"

type Submission struct {
	gorm.Model
	UserID      uint    `gorm:"index" json:"user_id,omitempty"`
	UserName    string  `json:"user_name,omitempty"`
	ProblemID   uint    `gorm:"index" json:"problem_id,omitempty"`
	ProblemName string  `json:"problem_name,omitempty"`
	LanguageID  uint    `gorm:"index" json:"language_id,omitempty"`
	Language    string  `json:"language,omitempty"`
	ContestID   uint    `gorm:"index" gorm:"default:0;0表示非比赛提交" json:"contest_id,omitempty"`
	SourceCode  string  `gorm:"type:longtext;comment:源代码'" json:"source_code,omitempty"`
	Status      string  `gorm:"type:varchar(255);comment:测评状态'" json:"status,omitempty"`
	Time        float64 `gorm:"comment:测评耗时" json:"time,omitempty"`
	Memory      float64 `gorm:"comment:测评耗存" json:"memory,omitempty"`
	Stderr      string  `gorm:"type:longtext;comment:标准错误'" json:"stderr,omitempty"`
	CompileOut  string  `gorm:"type:longtext;comment:编译输出'" json:"compile_out,omitempty"`
	Visible     *bool   `gorm:"default:true;comment:是否可见" json:"visible,omitempty"`
}

func (Submission) TableName() string {
	return "submission"
}
