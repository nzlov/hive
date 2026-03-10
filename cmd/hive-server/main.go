package main

import (
	"context"
	"log"

	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/memory"
	"github.com/nzlov/hive/internal/models"
	"github.com/nzlov/hive/internal/server"
	"github.com/nzlov/hive/internal/user"
	"github.com/robfig/cron/v3"
)

// main 负责组装配置、服务和路由，让服务端入口保持单一职责。
func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	store, err := models.Open(cfg)
	if err != nil {
		log.Fatalf("初始化数据库连接失败: %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			log.Printf("关闭数据库连接失败: %v", closeErr)
		}
	}()
	ctx := models.StoreToContext(context.Background(), store)
	service := memory.NewService(cfg)
	userService := user.NewService(cfg)
	defaultAdmin, createdPassword, err := userService.EnsureDefaultAdmin(ctx)
	if err != nil {
		log.Fatalf("初始化默认管理员失败: %v", err)
	}
	if createdPassword != "" {
		log.Printf("已创建默认管理员 username=%s userid=%s password=%s", defaultAdmin.Username, defaultAdmin.UserID, createdPassword)
	}
	statsService := server.NewDashboardStatsService()
	if err := statsService.Bootstrap(store); err != nil {
		log.Fatalf("初始化总览统计失败: %v", err)
	}
	result, err := service.EnsureEmbeddingsReady(ctx)
	if err != nil {
		log.Fatalf("校验嵌入模型失败: %v", err)
	}
	if result.Message != "" {
		log.Printf("嵌入模型检查完成: %s", result.Message)
	}
	if err := service.EnsureProtectedTagsSeeded(ctx); err != nil {
		log.Fatalf("初始化保护标签失败: %v", err)
	}
	cronScheduler := cron.New()
	if cfg.ScheduleConfig != nil && cfg.ScheduleConfig.MemoryCleanup.Enabled {
		if _, err := cronScheduler.AddFunc(cfg.ScheduleConfig.MemoryCleanup.Spec, func() {
			jobCtx := models.StoreToContext(context.Background(), store)
			result, err := service.RunMemoryCleanupOnce(jobCtx)
			if err != nil {
				log.Printf("记忆清理任务失败: %v", err)
				return
			}
			log.Printf("记忆清理任务完成: %s", result.Message)
		}); err != nil {
			log.Fatalf("注册记忆清理任务失败: %v", err)
		}
		cronScheduler.Start()
		defer cronScheduler.Stop()
	}
	router := server.NewRouter(service, userService, store, statsService)
	log.Printf("hive server listening on %s, %s", cfg.ServerListenAddr, cfg.String())
	if err := router.Run(cfg.ServerListenAddr); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
