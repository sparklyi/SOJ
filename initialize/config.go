package initialize

import (
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func InitConfig() {
	//go run main.go --config ./config.env 未携带此参数默认读取config/dev.yaml
	setting := pflag.String("config", "config/dev.yaml", "set config file dir")
	pflag.Parse()
	viper.SetConfigFile(*setting)
	if err := viper.ReadInConfig(); err != nil {
		panic(err)
	}

}
