package main

import (
	"SOJ/initialize"
	"context"
	"github.com/spf13/viper"
)

func main() {
	//配置初始化
	initialize.InitConfig()

	//服务初始化
	f := initialize.InitServer()
	//gin.SetMode(gin.ReleaseMode)
	//启动消费者组

	for _, c := range f.Consumer {
		go c.Consume(context.Background())
	}

	//启动定时任务
	f.Cron.Start()

	if err := f.G.Run(viper.GetString("server.port")); err != nil {
		panic("SOJ启动失败:" + err.Error())
	}

}
