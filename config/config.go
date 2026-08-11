package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type MainConfig struct {
	Port                    int    `toml:"port"`
	AppName                 string `toml:"appName"`
	Host                    string `toml:"host"`
	ReadTimeoutSeconds      int    `toml:"readTimeoutSeconds"`
	IdleTimeoutSeconds      int    `toml:"idleTimeoutSeconds"`
	ShutdownTimeoutSeconds  int    `toml:"shutdownTimeoutSeconds"`
	AIRequestTimeoutSeconds int    `toml:"aiRequestTimeoutSeconds"`
}

type EmailConfig struct {
	Authcode string `toml:"authcode"`
	Email    string `toml:"email" `
}

type RedisConfig struct {
	RedisPort     int    `toml:"port"`
	RedisDb       int    `toml:"db"`
	RedisHost     string `toml:"host"`
	RedisPassword string `toml:"password"`
}

type MysqlConfig struct {
	MysqlPort         int    `toml:"port"`
	MysqlHost         string `toml:"host"`
	MysqlUser         string `toml:"user"`
	MysqlPassword     string `toml:"password"`
	MysqlDatabaseName string `toml:"databaseName"`
	MysqlCharset      string `toml:"charset"`
}

type JwtConfig struct {
	ExpireDuration int    `toml:"expire_duration"`
	Issuer         string `toml:"issuer"`
	Subject        string `toml:"subject"`
	Key            string `toml:"key"`
}

type KafkaConfig struct {
	Brokers       []string `toml:"brokers"`
	MessageTopic  string   `toml:"messageTopic"`
	ConsumerGroup string   `toml:"consumerGroup"`
}

type RagModelConfig struct {
	RagEmbeddingModel string  `toml:"embeddingModel"`
	RagChatModelName  string  `toml:"chatModelName"`
	RagDocDir         string  `toml:"docDir"`
	RagBaseUrl        string  `toml:"baseUrl"`
	RagDimension      int     `toml:"dimension"`
	ChunkTokens       int     `toml:"chunkTokens"`
	ChunkOverlap      int     `toml:"chunkOverlap"`
	TopK              int     `toml:"topK"`
	MaxContextTokens  int     `toml:"maxContextTokens"`
	DistanceThreshold float64 `toml:"distanceThreshold"`
}

type AIModelConfig struct {
	Temperature             float64 `toml:"temperature"`
	MaxContextTokens        int     `toml:"maxContextTokens"`
	MaxOutputTokens         int     `toml:"maxOutputTokens"`
	RecentTurns             int     `toml:"recentTurns"`
	RetryMax                int     `toml:"retryMax"`
	CircuitFailureThreshold int     `toml:"circuitFailureThreshold"`
	CircuitOpenSeconds      int     `toml:"circuitOpenSeconds"`
}

type MCPConfig struct {
	BaseURL            string `toml:"baseUrl"`
	ToolTimeoutSeconds int    `toml:"toolTimeoutSeconds"`
	MaxToolRounds      int    `toml:"maxToolRounds"`
	MaxToolOutputBytes int    `toml:"maxToolOutputBytes"`
}

type VoiceServiceConfig struct {
	VoiceServiceApiKey    string `toml:"voiceServiceApiKey"`
	VoiceServiceSecretKey string `toml:"voiceServiceSecretKey"`
}

type Config struct {
	EmailConfig        `toml:"emailConfig"`
	RedisConfig        `toml:"redisConfig"`
	MysqlConfig        `toml:"mysqlConfig"`
	JwtConfig          `toml:"jwtConfig"`
	MainConfig         `toml:"mainConfig"`
	KafkaConfig        `toml:"kafkaConfig"`
	RagModelConfig     `toml:"ragModelConfig"`
	AIModelConfig      `toml:"aiModelConfig"`
	MCPConfig          `toml:"mcpConfig"`
	VoiceServiceConfig `toml:"voiceServiceConfig"`
}

type RedisKeyConfig struct {
	CaptchaPrefix   string
	IndexName       string
	IndexNamePrefix string
}

var DefaultRedisKeyConfig = RedisKeyConfig{
	CaptchaPrefix:   "captcha:%s",
	IndexName:       "rag_docs:%s:idx",
	IndexNamePrefix: "rag_docs:%s:",
}

var config *Config

// [第一阶段优化-配置] 加载 TOML、应用环境变量并校验关键配置。
func InitConfig() error {
	cfg := new(Config)
	if _, err := toml.DecodeFile("config/config.toml", cfg); err != nil {
		return err
	}
	if err := applyEnvironment(cfg); err != nil {
		return err
	}
	if err := validate(cfg); err != nil {
		return err
	}
	config = cfg
	return nil
}

func GetConfig() *Config {
	if config == nil {
		if err := InitConfig(); err != nil {
			log.Panicf("load config: %v", err)
		}
	}
	return config
}

// [第一阶段优化-配置] 敏感值和部署差异由环境变量覆盖。
func applyEnvironment(cfg *Config) error {
	setString(&cfg.MainConfig.Host, "APP_HOST")
	setString(&cfg.MainConfig.AppName, "APP_NAME")
	setString(&cfg.EmailConfig.Email, "SMTP_EMAIL")
	setString(&cfg.EmailConfig.Authcode, "SMTP_PASSWORD")
	setString(&cfg.RedisConfig.RedisHost, "REDIS_HOST")
	setString(&cfg.RedisConfig.RedisPassword, "REDIS_PASSWORD")
	setString(&cfg.MysqlConfig.MysqlHost, "MYSQL_HOST")
	setString(&cfg.MysqlConfig.MysqlUser, "MYSQL_USER")
	setString(&cfg.MysqlConfig.MysqlPassword, "MYSQL_PASSWORD")
	setString(&cfg.MysqlConfig.MysqlDatabaseName, "MYSQL_DATABASE")
	setString(&cfg.MysqlConfig.MysqlCharset, "MYSQL_CHARSET")
	setString(&cfg.JwtConfig.Issuer, "JWT_ISSUER")
	setString(&cfg.JwtConfig.Subject, "JWT_SUBJECT")
	setString(&cfg.JwtConfig.Key, "JWT_SECRET")
	setString(&cfg.KafkaConfig.MessageTopic, "KAFKA_TOPIC")
	setString(&cfg.KafkaConfig.ConsumerGroup, "KAFKA_CONSUMER_GROUP")
	setString(&cfg.RagModelConfig.RagEmbeddingModel, "RAG_EMBEDDING_MODEL")
	setString(&cfg.RagModelConfig.RagChatModelName, "RAG_CHAT_MODEL")
	setString(&cfg.RagModelConfig.RagDocDir, "RAG_DOC_DIR")
	setString(&cfg.RagModelConfig.RagBaseUrl, "RAG_BASE_URL")
	setString(&cfg.MCPConfig.BaseURL, "MCP_BASE_URL")
	setString(&cfg.VoiceServiceConfig.VoiceServiceApiKey, "VOICE_SERVICE_API_KEY")
	setString(&cfg.VoiceServiceConfig.VoiceServiceSecretKey, "VOICE_SERVICE_SECRET_KEY")

	var err error
	if cfg.MainConfig.Port, err = envInt("APP_PORT", cfg.MainConfig.Port); err != nil {
		return err
	}
	// [第三阶段优化-超时] HTTP、AI 调用和优雅退出时限均可由部署环境覆盖。
	if cfg.MainConfig.ReadTimeoutSeconds, err = envInt("HTTP_READ_TIMEOUT_SECONDS", cfg.MainConfig.ReadTimeoutSeconds); err != nil {
		return err
	}
	if cfg.MainConfig.IdleTimeoutSeconds, err = envInt("HTTP_IDLE_TIMEOUT_SECONDS", cfg.MainConfig.IdleTimeoutSeconds); err != nil {
		return err
	}
	if cfg.MainConfig.ShutdownTimeoutSeconds, err = envInt("HTTP_SHUTDOWN_TIMEOUT_SECONDS", cfg.MainConfig.ShutdownTimeoutSeconds); err != nil {
		return err
	}
	if cfg.MainConfig.AIRequestTimeoutSeconds, err = envInt("AI_REQUEST_TIMEOUT_SECONDS", cfg.MainConfig.AIRequestTimeoutSeconds); err != nil {
		return err
	}
	if cfg.RedisConfig.RedisPort, err = envInt("REDIS_PORT", cfg.RedisConfig.RedisPort); err != nil {
		return err
	}
	if cfg.RedisConfig.RedisDb, err = envInt("REDIS_DB", cfg.RedisConfig.RedisDb); err != nil {
		return err
	}
	if cfg.MysqlConfig.MysqlPort, err = envInt("MYSQL_PORT", cfg.MysqlConfig.MysqlPort); err != nil {
		return err
	}
	if cfg.JwtConfig.ExpireDuration, err = envInt("JWT_EXPIRE_HOURS", cfg.JwtConfig.ExpireDuration); err != nil {
		return err
	}
	if cfg.RagModelConfig.RagDimension, err = envInt("RAG_DIMENSION", cfg.RagModelConfig.RagDimension); err != nil {
		return err
	}
	for name, target := range map[string]*int{
		"AI_MAX_CONTEXT_TOKENS":     &cfg.AIModelConfig.MaxContextTokens,
		"AI_MAX_OUTPUT_TOKENS":      &cfg.AIModelConfig.MaxOutputTokens,
		"AI_RECENT_TURNS":           &cfg.AIModelConfig.RecentTurns,
		"AI_RETRY_MAX":              &cfg.AIModelConfig.RetryMax,
		"AI_CIRCUIT_FAILURES":       &cfg.AIModelConfig.CircuitFailureThreshold,
		"AI_CIRCUIT_OPEN_SECONDS":   &cfg.AIModelConfig.CircuitOpenSeconds,
		"RAG_CHUNK_TOKENS":          &cfg.RagModelConfig.ChunkTokens,
		"RAG_CHUNK_OVERLAP":         &cfg.RagModelConfig.ChunkOverlap,
		"RAG_TOP_K":                 &cfg.RagModelConfig.TopK,
		"RAG_MAX_CONTEXT_TOKENS":    &cfg.RagModelConfig.MaxContextTokens,
		"MCP_TOOL_TIMEOUT_SECONDS":  &cfg.MCPConfig.ToolTimeoutSeconds,
		"MCP_MAX_TOOL_ROUNDS":       &cfg.MCPConfig.MaxToolRounds,
		"MCP_MAX_TOOL_OUTPUT_BYTES": &cfg.MCPConfig.MaxToolOutputBytes,
	} {
		if *target, err = envInt(name, *target); err != nil {
			return err
		}
	}
	if cfg.AIModelConfig.Temperature, err = envFloat("AI_TEMPERATURE", cfg.AIModelConfig.Temperature); err != nil {
		return err
	}
	if cfg.RagModelConfig.DistanceThreshold, err = envFloat("RAG_DISTANCE_THRESHOLD", cfg.RagModelConfig.DistanceThreshold); err != nil {
		return err
	}
	if value := strings.TrimSpace(os.Getenv("KAFKA_BROKERS")); value != "" {
		cfg.KafkaConfig.Brokers = splitNonEmpty(value)
	}
	return nil
}

// [第一阶段优化-配置] 配置错误在启动阶段快速失败。
func validate(cfg *Config) error {
	missing := make([]string, 0)
	for name, value := range map[string]string{
		"MYSQL_HOST":        cfg.MysqlConfig.MysqlHost,
		"MYSQL_USER":        cfg.MysqlConfig.MysqlUser,
		"MYSQL_DATABASE":    cfg.MysqlConfig.MysqlDatabaseName,
		"JWT_SECRET":        cfg.JwtConfig.Key,
		"DASHSCOP_API_KEY":  os.Getenv("DASHSCOP_API_KEY"),
		"DASHSCOP_BASE_URL": os.Getenv("DASHSCOP_BASE_URL"),
		"OPENAI_MODEL_NAME": os.Getenv("OPENAI_MODEL_NAME"),
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
	if len(cfg.JwtConfig.Key) < 32 {
		return fmt.Errorf("JWT_SECRET must contain at least 32 characters")
	}
	if cfg.JwtConfig.ExpireDuration <= 0 || cfg.JwtConfig.ExpireDuration > 168 {
		return fmt.Errorf("JWT_EXPIRE_HOURS must be between 1 and 168")
	}
	if cfg.MainConfig.Port <= 0 || cfg.MainConfig.Port > 65535 {
		return fmt.Errorf("APP_PORT must be between 1 and 65535")
	}
	// [第三阶段优化-超时] 防止错误配置造成请求永久占用连接或立即超时。
	for name, value := range map[string]int{
		"HTTP_READ_TIMEOUT_SECONDS":     cfg.MainConfig.ReadTimeoutSeconds,
		"HTTP_IDLE_TIMEOUT_SECONDS":     cfg.MainConfig.IdleTimeoutSeconds,
		"HTTP_SHUTDOWN_TIMEOUT_SECONDS": cfg.MainConfig.ShutdownTimeoutSeconds,
		"AI_REQUEST_TIMEOUT_SECONDS":    cfg.MainConfig.AIRequestTimeoutSeconds,
	} {
		if value <= 0 || value > 3600 {
			return fmt.Errorf("%s must be between 1 and 3600", name)
		}
	}
	if cfg.AIModelConfig.MaxContextTokens <= cfg.AIModelConfig.MaxOutputTokens ||
		cfg.AIModelConfig.MaxOutputTokens <= 0 || cfg.AIModelConfig.RecentTurns <= 0 {
		return fmt.Errorf("AI token limits and recent turns are invalid")
	}
	if cfg.RagModelConfig.ChunkTokens <= 0 || cfg.RagModelConfig.ChunkOverlap < 0 ||
		cfg.RagModelConfig.ChunkOverlap >= cfg.RagModelConfig.ChunkTokens {
		return fmt.Errorf("RAG chunk configuration is invalid")
	}
	if cfg.MCPConfig.BaseURL == "" || cfg.MCPConfig.MaxToolRounds <= 0 || cfg.MCPConfig.ToolTimeoutSeconds <= 0 {
		return fmt.Errorf("MCP configuration is invalid")
	}
	return nil
}

// [第一阶段优化-配置] 以下辅助函数负责类型安全的环境变量解析。
func setString(target *string, name string) {
	if value, ok := os.LookupEnv(name); ok {
		*target = strings.TrimSpace(value)
	}
}

func envInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}

func envFloat(name string, fallback float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", name, err)
	}
	return parsed, nil
}

func splitNonEmpty(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}
