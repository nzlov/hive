package server

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nzlov/hive/internal/api"
	"github.com/nzlov/hive/internal/models"
)

const dashboardHotTagTopLimit = 10

// dashboardStatsService 在内存中维护总览统计，避免每次打开页面都做全量数据库聚合。
type dashboardStatsService struct {
	userTotal    int64
	memoryTotal  int64
	summaryTotal int64
	errorTotal   int64
	tagCounter   sync.Map
}

// NewDashboardStatsService 创建统计服务并初始化计数容器，避免后续更新出现 nil map 分支。
func NewDashboardStatsService() *dashboardStatsService {
	return &dashboardStatsService{}
}

// Bootstrap 从数据库加载一次基础统计，为服务启动后的内存累计提供初始基线。
func (s *dashboardStatsService) Bootstrap(store *models.Store) error {
	userTotal, err := store.CountUsers()
	if err != nil {
		return err
	}
	memories, err := store.ListAllMemories()
	if err != nil {
		return err
	}
	summaryTotal := int64(0)
	errorTotal := int64(0)
	tagCounter := map[string]int64{}
	for _, memoryItem := range memories {
		memoryType := strings.TrimSpace(memoryItem.Type)
		if memoryType == "summary" {
			summaryTotal++
		}
		if memoryType == "error" {
			errorTotal++
		}
		for _, tag := range models.DecodeTags(memoryItem.Tags) {
			normalized := strings.TrimSpace(tag)
			if normalized == "" {
				continue
			}
			tagCounter[normalized]++
		}
	}
	atomic.StoreInt64(&s.userTotal, userTotal)
	atomic.StoreInt64(&s.memoryTotal, int64(len(memories)))
	atomic.StoreInt64(&s.summaryTotal, summaryTotal)
	atomic.StoreInt64(&s.errorTotal, errorTotal)
	s.tagCounter = sync.Map{}
	for tag, count := range tagCounter {
		counter := new(int64)
		atomic.StoreInt64(counter, count)
		s.tagCounter.Store(tag, counter)
	}
	return nil
}

// OnUserCreated 在新增用户后递增用户统计，避免前端总览需要等待下一次全量刷新。
func (s *dashboardStatsService) OnUserCreated() {
	atomic.AddInt64(&s.userTotal, 1)
}

// OnUserDeleted 在删除用户后递减用户统计，并确保计数不出现负数。
func (s *dashboardStatsService) OnUserDeleted() {
	atomicDecreaseNoNegative(&s.userTotal)
}

// OnMemoryWritten 在写入记忆后增量更新总数、类型分布和标签热度，避免全量重算。
func (s *dashboardStatsService) OnMemoryWritten(items []api.MemoryWriteItem) {
	if len(items) == 0 {
		return
	}
	for _, item := range items {
		atomic.AddInt64(&s.memoryTotal, 1)
		memoryType := strings.TrimSpace(item.Type)
		if memoryType == "summary" {
			atomic.AddInt64(&s.summaryTotal, 1)
		}
		if memoryType == "error" {
			atomic.AddInt64(&s.errorTotal, 1)
		}
		for _, tag := range item.Tags {
			normalized := strings.TrimSpace(tag)
			if normalized == "" {
				continue
			}
			s.increaseTagCount(normalized)
		}
	}
}

// OnMemoryDeleted 在删除记忆后回收基础统计，保证管理端删改后总览即时一致。
func (s *dashboardStatsService) OnMemoryDeleted(memoryItem models.Memory) {
	atomicDecreaseNoNegative(&s.memoryTotal)
	memoryType := strings.TrimSpace(memoryItem.Type)
	if memoryType == "summary" {
		atomicDecreaseNoNegative(&s.summaryTotal)
	}
	if memoryType == "error" {
		atomicDecreaseNoNegative(&s.errorTotal)
	}
	for _, tag := range models.DecodeTags(memoryItem.Tags) {
		normalized := strings.TrimSpace(tag)
		if normalized == "" {
			continue
		}
		s.decreaseTagCount(normalized)
	}
}

// Snapshot 导出基础统计快照，避免控制器层直接持锁组装响应。
func (s *dashboardStatsService) Snapshot() api.DashboardBaseStats {
	userTotal := atomic.LoadInt64(&s.userTotal)
	memoryTotal := atomic.LoadInt64(&s.memoryTotal)
	summaryTotal := atomic.LoadInt64(&s.summaryTotal)
	errorTotal := atomic.LoadInt64(&s.errorTotal)
	hotTags := s.topTags(dashboardHotTagTopLimit)
	return api.DashboardBaseStats{
		UserTotal:      userTotal,
		MemoryTotal:    memoryTotal,
		MemoryType:     api.DashboardMemoryTypeStat{Summary: summaryTotal, Error: errorTotal},
		HotTags:        hotTags,
		HotTagTopLimit: dashboardHotTagTopLimit,
	}
}

// topTags 返回热门标签 TopN，确保展示顺序稳定且可解释。
func (s *dashboardStatsService) topTags(topLimit int) []api.DashboardTagStat {
	items := make([]api.DashboardTagStat, 0, topLimit)
	s.tagCounter.Range(func(key, value any) bool {
		tag, ok := key.(string)
		if !ok {
			return true
		}
		counter, ok := value.(*int64)
		if !ok || counter == nil {
			return true
		}
		count := atomic.LoadInt64(counter)
		if count <= 0 {
			return true
		}
		items = append(items, api.DashboardTagStat{Tag: tag, Count: count})
		return true
	})
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Tag < items[j].Tag
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > topLimit {
		items = items[:topLimit]
	}
	return items
}

// increaseTagCount 原子递增标签计数，避免并发写入时覆盖旧值。
func (s *dashboardStatsService) increaseTagCount(tag string) {
	for {
		value, ok := s.tagCounter.Load(tag)
		if !ok {
			counter := new(int64)
			if actual, loaded := s.tagCounter.LoadOrStore(tag, counter); !loaded {
				atomic.AddInt64(counter, 1)
				return
			} else {
				value = actual
			}
		}
		counter, ok := value.(*int64)
		if !ok || counter == nil {
			s.tagCounter.Delete(tag)
			continue
		}
		atomic.AddInt64(counter, 1)
		return
	}
}

// decreaseTagCount 原子递减标签计数并下限保护，避免删除流程将计数减为负数。
func (s *dashboardStatsService) decreaseTagCount(tag string) {
	value, ok := s.tagCounter.Load(tag)
	if !ok {
		return
	}
	counter, ok := value.(*int64)
	if !ok || counter == nil {
		s.tagCounter.Delete(tag)
		return
	}
	atomicDecreaseNoNegative(counter)
}

// atomicDecreaseNoNegative 原子递减并保证最小为 0，避免并发场景出现负数统计。
func atomicDecreaseNoNegative(counter *int64) {
	for {
		current := atomic.LoadInt64(counter)
		if current <= 0 {
			return
		}
		if atomic.CompareAndSwapInt64(counter, current, current-1) {
			return
		}
	}
}

// formatBytesHuman 把字节数转成人类可读格式，便于总览页直观展示缓存体量。
func formatBytesHuman(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	unites := []string{"KB", "MB", "GB", "TB"}
	size := float64(value)
	idx := 0
	for size >= 1024 && idx < len(unites)-1 {
		size /= 1024
		idx++
	}
	return fmt.Sprintf("%.2f %s", size, unites[idx])
}
