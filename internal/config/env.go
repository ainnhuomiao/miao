package config

import "os"

const (
	// EnvPrefix 是程序自身环境变量的前缀。
	EnvPrefix = "MIAO_"
	// OldEnvPrefix 是改名前的前缀，仅作回退读取。
	OldEnvPrefix = "AGNET_"
	// LegacyAgentPrefix 是连接配置（API_KEY、BASE_URL、MODEL、WORKSPACE）的旧前缀，仅作回退读取。
	LegacyAgentPrefix = "AGENT_"
)

// AgentEnvNames 返回连接配置 key 的候选环境变量名，按优先级排列：MIAO_<key>，然后旧名 AGENT_<key>。
// 与 firstEnv 配合使用：取第一个非空值。
func AgentEnvNames(key string) []string {
	return []string{EnvPrefix + key, LegacyAgentPrefix + key}
}

// Getenv 读取程序自身的环境变量：先看 MIAO_<key>，只要设置了（哪怕为空）就以它为准；
// 否则回退到旧名 AGNET_<key>。
func Getenv(key string) string {
	if v, ok := os.LookupEnv(EnvPrefix + key); ok {
		return v
	}
	return os.Getenv(OldEnvPrefix + key)
}
