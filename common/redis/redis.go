package redis

import (
	"GoAI/config"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	redisCli "github.com/redis/go-redis/v9"
)

var Rdb *redisCli.Client

var ctx = context.Background()

func Init() error {
	conf := config.GetConfig()
	host := conf.RedisConfig.RedisHost
	port := conf.RedisConfig.RedisPort
	password := conf.RedisConfig.RedisPassword
	db := conf.RedisDb
	addr := host + ":" + strconv.Itoa(port)

	Rdb = redisCli.NewClient(&redisCli.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		Protocol: 2, // 使用 Protocol 2 避免 maint_notifications 警告
	})
	// [第一阶段优化-可用性] 创建客户端后立即探测连接，避免假启动。
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Rdb.Ping(pingCtx).Err(); err != nil {
		_ = Rdb.Close()
		Rdb = nil
		return fmt.Errorf("connect to Redis: %w", err)
	}
	return nil
}

// [第三阶段优化-生命周期] 统一关闭 Redis 客户端。
func Close() error {
	if Rdb == nil {
		return nil
	}
	return Rdb.Close()
}

func SetCaptchaForEmail(email, captcha string) error {
	key := GenerateCaptcha(email)
	expire := 2 * time.Minute //过期时间，设置为2分钟
	return Rdb.Set(ctx, key, captcha, expire).Err()
}

func CheckCaptchaForEmail(email, userInput string) (bool, error) {
	key := GenerateCaptcha(email)

	storedCaptcha, err := Rdb.Get(ctx, key).Result()
	if err != nil {
		if err == redisCli.Nil {

			return false, nil
		}

		return false, err
	}
	// 忽略大小写比较
	if strings.EqualFold(storedCaptcha, userInput) {

		// 验证成功后删除 key
		if err := Rdb.Del(ctx, key).Err(); err != nil {
			return false, err
		} else {

		}
		return true, nil
	}

	return false, nil
}

// InitRedisIndex 初始化用户级知识库索引。
func InitRedisIndex(ctx context.Context, scope string, dimension int) error {
	indexName := GenerateIndexName(scope)

	// 检查索引是否存在
	_, err := Rdb.Do(ctx, "FT.INFO", indexName).Result()
	if err == nil {
		fmt.Println("索引已存在，跳过创建")
		return nil
	}

	// 如果索引不存在，创建新索引
	if !isRedisIndexNotFoundError(err) {
		return fmt.Errorf("检查索引失败: %w", err)
	}

	fmt.Println("正在创建 Redis 索引...")

	prefix := GenerateIndexNamePrefix(scope)

	// 创建索引
	createArgs := []interface{}{
		"FT.CREATE", indexName,
		"ON", "HASH",
		"PREFIX", "1", prefix,
		"SCHEMA",
		"content", "TEXT",
		"metadata", "TEXT",
		"document_id", "TAG",
		"source", "TEXT",
		"chunk_index", "NUMERIC",
		"vector", "VECTOR", "FLAT",
		"6",
		"TYPE", "FLOAT32",
		"DIM", dimension,
		"DISTANCE_METRIC", "COSINE",
	}

	if err := Rdb.Do(ctx, createArgs...).Err(); err != nil {
		return fmt.Errorf("创建索引失败: %w", err)
	}

	fmt.Println("索引创建成功！")
	return nil
}

func isRedisIndexNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unknown index name") ||
		strings.Contains(message, "search_index_not_found") ||
		strings.Contains(message, "index not found")
}

func DeleteDocumentChunks(ctx context.Context, scope, documentID string) error {
	pattern := GenerateIndexNamePrefix(scope) + documentID + ":*"
	var cursor uint64
	for {
		keys, next, err := Rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := Rdb.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func ReplaceDocumentChunks(ctx context.Context, scope, temporaryID, documentID string) error {
	prefix := GenerateIndexNamePrefix(scope)
	temporaryPattern := prefix + temporaryID + ":*"
	var temporaryKeys []string
	var cursor uint64
	for {
		keys, next, err := Rdb.Scan(ctx, cursor, temporaryPattern, 100).Result()
		if err != nil {
			return err
		}
		temporaryKeys = append(temporaryKeys, keys...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if len(temporaryKeys) == 0 {
		return fmt.Errorf("temporary document index is empty")
	}
	hashes := make(map[string]map[string]string, len(temporaryKeys))
	for _, key := range temporaryKeys {
		fields, err := Rdb.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		fields["document_id"] = documentID
		stableKey := strings.Replace(key, prefix+temporaryID+":", prefix+documentID+":", 1)
		hashes[stableKey] = fields
	}
	var stableKeys []string
	cursor = 0
	for {
		keys, next, err := Rdb.Scan(ctx, cursor, prefix+documentID+":*", 100).Result()
		if err != nil {
			return err
		}
		stableKeys = append(stableKeys, keys...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	_, err := Rdb.TxPipelined(ctx, func(pipe redisCli.Pipeliner) error {
		if len(stableKeys) > 0 {
			pipe.Del(ctx, stableKeys...)
		}
		for stableKey, fields := range hashes {
			pipe.HSet(ctx, stableKey, fields)
		}
		pipe.Del(ctx, temporaryKeys...)
		return nil
	})
	return err
}

// DeleteRedisIndex 删除 Redis 索引，支持按文件名区分
func DeleteRedisIndex(ctx context.Context, filename string) error {
	indexName := GenerateIndexName(filename)

	// 删除索引
	if err := Rdb.Do(ctx, "FT.DROPINDEX", indexName).Err(); err != nil {
		return fmt.Errorf("删除索引失败: %w", err)
	}

	fmt.Println("索引删除成功！")
	return nil
}
