package model

type Language struct {
	ID     uint   `gorm:"primary_key" json:"id,omitempty"`
	Name   string `gorm:"type:varchar(255);not null" json:"name,omitempty"`
	Status *bool  `gorm:"default:false;comment:是否启用" json:"status,omitempty"`

	//外键
	Submission []Submission `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"submission,omitempty"`
}

func (Language) TableName() string {
	return "language"
}
