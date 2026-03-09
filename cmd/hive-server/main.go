package main

import (
	"log"

	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/memory"
	"github.com/nzlov/hive/internal/server"
)

// main 负责组装配置、服务和路由，让服务端入口保持单一职责。
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	service := memory.NewService(cfg)
	result, err := service.EnsureEmbeddingsReady()
	if err != nil {
		log.Fatalf("校验嵌入模型失败: %v", err)
	}
	if result.Message != "" {
		log.Printf("嵌入模型检查完成: %s", result.Message)
	}
	router := server.NewRouter(service)
	log.Printf("hive server listening on %s, %s", cfg.ServerListenAddr, cfg.String())
	if err := router.Run(cfg.ServerListenAddr); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
