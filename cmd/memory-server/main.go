package main

import (
	"log"

	"memory-manager/internal/config"
	"memory-manager/internal/memory"
	"memory-manager/internal/server"
)

// main 负责组装配置、服务和路由，让服务端入口保持单一职责。
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	service := memory.NewService(cfg)
	router := server.NewRouter(service)
	log.Printf("memory server listening on %s, %s", cfg.ServerListenAddr, cfg.String())
	if err := router.Run(cfg.ServerListenAddr); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
