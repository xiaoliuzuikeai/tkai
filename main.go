package main

import (
	"GoAI/common/aihelper"
	"GoAI/common/mysql"
	"GoAI/common/redis"
	"GoAI/config"
	"GoAI/router"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

// [第三阶段优化-生命周期] 使用可控的 HTTP Server，支持超时配置和优雅退出。
func StartServer(ctx context.Context, addr string, port int) error {
	conf := config.GetConfig().MainConfig
	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", addr, port),
		Handler:           router.InitRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       time.Duration(conf.ReadTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(conf.IdleTimeoutSeconds) * time.Second,
		// SSE 连接不设置 WriteTimeout，由 AIRequestTimeoutSeconds 控制总时长。
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("HTTP server listening on %s", server.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(conf.ShutdownTimeoutSeconds)*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func main() {
	// [第一阶段优化-配置] 优先加载本机私密配置，再加载通用 .env。
	if err := godotenv.Load(".env.local", ".env"); err != nil {
		log.Println("environment files not found; using system environment variables")
	}

	// [第一阶段优化-配置] 启动基础设施前完成环境变量覆盖和必填项校验。
	if err := config.InitConfig(); err != nil {
		log.Printf("configuration error: %v", err)
		return
	}
	conf := config.GetConfig()
	host := conf.MainConfig.Host
	port := conf.MainConfig.Port
	//初始化mysql
	if err := mysql.InitMysql(); err != nil {
		log.Println("InitMysql error , " + err.Error())
		return
	}
	defer func() {
		if err := mysql.Close(); err != nil {
			log.Printf("close mysql: %v", err)
		}
	}()
	defer aihelper.GetGlobalManager().Close()
	// [第二阶段优化-缓存] AIHelper 按会话首次访问懒加载，不再启动时读取全部消息。

	//初始化redis
	// [第一阶段优化-可用性] Redis 初始化包含真实连接探测。
	if err := redis.Init(); err != nil {
		log.Println("InitRedis error, " + err.Error())
		return
	}
	log.Println("redis init success  ")
	defer func() {
		if err := redis.Close(); err != nil {
			log.Printf("close redis: %v", err)
		}
	}()
	// [第二阶段优化-持久化] 聊天消息同步写库，Kafka 不再是服务启动的强依赖。

	// [第三阶段优化-生命周期] 收到退出信号后停止接收新请求，并等待在途请求结束。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := StartServer(ctx, host, port); err != nil {
		log.Printf("HTTP server stopped with error: %v", err)
		return
	}
	log.Println("HTTP server stopped gracefully")
}
