package model

type Language struct {
	ID       uint   `gorm:"primary_key" json:"id,omitempty"`
	Name     string `gorm:"type:char(30);not null;comment:语言" json:"name,omitempty"`
	ActionID string `gorm:"type:char(30);not null;comment:语言版本" json:"action_id,omitempty"`
	Template string `gorm:"type:char(30);not null;comment:语言模板" json:"template,omitempty"`
	Status   *bool  `gorm:"default:false;comment:是否启用" json:"status,omitempty"`

	//外键
	Submission []Submission `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"submission,omitempty"`
}

func (Language) TableName() string {
	return "language"
}
