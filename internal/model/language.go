package model

type Language struct {
	ID     int    `gorm:"primary_key" json:"id"`
	Name   string `gorm:"type:varchar(255);not null" json:"name"`
	Status *bool  `gorm:"default:false;comment:是否启用" json:"status"`
}

func (Language) TableName() string {
	return "language"
}
