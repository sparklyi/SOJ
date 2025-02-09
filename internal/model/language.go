package model

type Language struct {
	ID     uint   `gorm:"primary_key" json:"id"`
	Name   string `gorm:"type:varchar(255);not null" json:"name"`
	Status *bool  `gorm:"default:false;comment:是否启用" json:"status"`

	//外键
	//Submission []Submission `gorm:"constraint:OnUpdate:CASCADE;OnDelete:SET NULL;" json:"submission,omitempty"`
}

func (Language) TableName() string {
	return "language"
}
