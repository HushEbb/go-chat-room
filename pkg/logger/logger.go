package logger

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 全局日志记录器实例
var L *zap.Logger

// `level`可以是“debug”、“info”、“warn”、“error”、“fatal”、“panic”。
// `isProduction`确定日志记录器是否使用JSON格式(生产)或控制台格式(开发)。
func InitLogger(level string, isProduction bool) error {
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.InfoLevel // 如果解析失败，则默认为Info级别
		fmt.Fprintf(os.Stderr, "Warning: Invalid log level '%s', using default 'info'. Error: %v\n", level, err)
	}

	var err error
	if isProduction {
		// 生产日志记录器：JSON格式，默认Info级别及以上
		config := zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zapLevel)
		L, err = config.Build(zap.AddCallerSkip(1))
	} else {
		// 开发日志记录器：人类可读的控制台格式，默认Debug级别及以上
		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder // 彩色级别输出
		config.Level = zap.NewAtomicLevelAt(zapLevel)
		L, err = config.Build(zap.AddCallerSkip(1))
	}

	if err != nil {
		return fmt.Errorf("failed to initialize zap logger: %w", err)
	}

	L.Info("Zap logger initialized", zap.String("level", zapLevel.String()), zap.Bool("productionMode", isProduction))
	return nil
}

// Sync刷新任何缓冲的日志条目。
// 建议在应用程序退出之前调用它。
func Sync() {
	if L != nil {
		_ = L.Sync()
	}
}
