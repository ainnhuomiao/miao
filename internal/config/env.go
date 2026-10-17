package config

import "os"

const (
	// EnvPrefix 是程序自身环境变量的前缀。
	EnvPrefix = "MIAO_"
	// OldEnvPrefix 是改名前的前缀，仅作回退读取。
	OldEnvPrefix = "AGNET_"
)

// Getenv 读取程序自身的环境变量：先看 MIAO_<key>，只要设置了（哪怕为空）就以它为准；
// 否则回退到旧名 AGNET_<key>。
func Getenv(key string) string {
	if v, ok := os.LookupEnv(EnvPrefix + key); ok {
		return v
	}
	return os.Getenv(OldEnvPrefix + key)
}
