package initialize

import (
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func InitConfig() {
	//go run main.go --config ./dev.yaml 未携带此参数默认读取config/config.yaml
	setting := pflag.String("config", "config/config.yaml", "set config file dir")
	//命令行参数解析
	pflag.Parse()
	viper.SetConfigFile(*setting)
	if err := viper.ReadInConfig(); err != nil {
		panic(err)
	}

}
