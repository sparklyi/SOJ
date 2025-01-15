package initialize

import (
	"SOJ/internal/mq"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"gorm.io/gorm"
)

type Cmd struct {
	G             *gin.Engine
	EmailConsumer *mq.EmailConsumer
	Mongo         *mongo.Database
	DB            *gorm.DB
}
