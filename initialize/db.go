package initialize

import (
	"SOJ/internal/model"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func InitDB() *gorm.DB {
	dsn := viper.GetString("mysql.dsn")
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	err = db.AutoMigrate(
		model.User{},
		model.Problem{},
		model.Language{},
		model.TestCase{},
		model.Submission{},
	)
	if err != nil {
		panic(err)
	}
	return db

}
