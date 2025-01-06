package logger

import (
	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// GetEncoder 获取编码器配置
func GetEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	return zapcore.NewConsoleEncoder(encoderConfig)
}

// GetLumberjack 日志切割归档
func GetLumberjack(f string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   f,    //日志文件位置
		MaxSize:    10,   //文件最大大小(MB)
		MaxBackups: 5,    //保留个数
		MaxAge:     30,   //保留天数
		Compress:   true, //是否压缩
		LocalTime:  true,
	}
}
