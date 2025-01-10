package initialize

import (
	"SOJ/pkg/logger"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"path/filepath"
	"time"
)

func InitLogger() *zap.Logger {
	path := viper.GetString("log.path")
	f := filepath.Join(path, time.Now().Format("2006-01-02")+".log")

	//多路输出
	wr := zapcore.NewMultiWriteSyncer(
		zapcore.AddSync(logger.GetLumberjack(f)),
		zapcore.AddSync(os.Stdout),
	)
	//获取编码器配置
	encoderConfig := logger.GetEncoder()

	//新建核心, 记录error及以上的日志信息
	core := zapcore.NewCore(encoderConfig, wr, zapcore.InfoLevel)

	//新建日志, 自动添加调用信息, 记录error及以上的堆栈信息
	return zap.New(
		core,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)

}
