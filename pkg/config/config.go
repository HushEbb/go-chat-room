package config

import (
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Log       LogConfig       `mapstructure:"log"`
	Database  DatabaseConfig  `mapstructure:"database"`
	JWT       JWTConfig       `mapstructure:"jwt"`
	WebSocket WebSocketConfig `mapstructure:"websocket"`
	Server    ServerConfig    `mapstructure:"server"`
}

type LogConfig struct {
	Level          string `mapstructure:"level"`
	ProductionMode bool   `mapstructure:"production_mode"`
}

type DatabaseConfig struct {
	DSN string `mapstructure:"dsn"`
}

type ServerConfig struct {
	Address string `mapstructure:"address"`
	GinMode string `mapstructure:"gin_mode"`
}

type JWTConfig struct {
	Secret     string        `mapstructure:"secret"`
	Expiration time.Duration `mapstructure:"expiration"`
}

type WebSocketConfig struct {
	BroadcastBufferSize int `mapstructure:"broadcast_buffer_size"`

	// 重试相关配置
	// TODO: still needed?
	MessageRetryCount      int `mapstructure:"message_retry_count"`
	MessageRetryIntervalMs int `mapstructure:"message_retry_interval_ms"`
}

var GlobalConfig Config

func Init() error {
	// 获取项目根目录
	_, b, _, _ := runtime.Caller(0)
	basepath := filepath.Dir(filepath.Dir(filepath.Dir(b)))

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(filepath.Join(basepath, "config"))

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := viper.Unmarshal(&GlobalConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return nil
}

// 测试用的配置文件
func InitTest() error {
	// 获取项目根目录
	_, b, _, _ := runtime.Caller(0)
	basepath := filepath.Dir(filepath.Dir(filepath.Dir(b)))

	viper.SetConfigName("config.test")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(filepath.Join(basepath, "config"))

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := viper.Unmarshal(&GlobalConfig); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return nil
}
