package initialize

import (
	"context"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func InitMongoDB() *mongo.Database {
	uri := viper.GetString("mongo.uri")
	cli, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(uri))
	if err != nil {
		panic(err)
	}
	err = cli.Ping(context.TODO(), nil)
	if err != nil {
		panic(err)
	}
	return cli.Database(viper.GetString("mongo.db"))

}
