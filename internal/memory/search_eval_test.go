package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nzlov/hive/internal/config"
)

// TestSearchEvalDataset 使用 testdata 下的数据跑一轮离线评测，防止搜索效果回归。
func TestSearchEvalDataset(t *testing.T) {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	dataset, experimentFile, err := LoadSearchEvalDataset(root)
	if err != nil {
		t.Fatalf("加载评测数据失败: %v", err)
	}
	runOptions, ok := loadSearchEvalTestRunOptions(t, root)
	if !ok {
		t.Skip("未配置外部 embedding，跳过真实模型评测测试")
	}
	if configPath := searchEvalTestConfigPath(root); configPath != "" {
		if appConfig, err := config.LoadFromPath(configPath); err == nil {
			experimentFile.Baseline = BuildSearchEvalScenarioFromAppConfig(appConfig)
		}
	}
	report, err := RunSearchEvalExperimentWithOptions(context.Background(), dataset, "baseline", experimentFile.Baseline, runOptions)
	if err != nil {
		t.Fatalf("执行基线评测失败: %v", err)
	}
	report.Passed, report.ValidationError = validateSearchEvalReport(report.Summary, report.Summary, experimentFile.Acceptance)
	t.Logf("search eval metrics: Recall@5=%.4f MRR@10=%.4f nDCG@10=%.4f Top1=%.4f ErrorRecall@5=%.4f P95=%.3fms", report.Summary.RecallAt5, report.Summary.MRRAt10, report.Summary.NDCGAt10, report.Summary.Top1Accuracy, report.Summary.ErrorRecallAt5, report.Summary.P95LatencyMS)
	for _, failure := range report.Failures {
		t.Logf("search eval failure: query_id=%s category=%s recall@5=%.4f mrr@10=%.4f top1=%v expected_top1=%v actual_top5=%v", failure.QueryID, failure.Category, failure.RecallAt5, failure.MRRAt10, failure.Top1Correct, failure.ExpectedTop1, failure.ActualTop5)
	}
	if !report.Passed {
		t.Fatalf("评测未达标: %v", report.ValidationError)
	}
}

// loadSearchEvalTestRunOptions 复用仓库配置中的嵌入服务，让测试在真实模型可用时自动执行。
func loadSearchEvalTestRunOptions(t *testing.T, root string) (SearchEvalRunOptions, bool) {
	t.Helper()
	configPath := searchEvalTestConfigPath(root)
	if configPath == "" {
		return SearchEvalRunOptions{}, false
	}
	appConfig, err := config.LoadFromPath(configPath)
	if err != nil {
		return SearchEvalRunOptions{}, false
	}
	if appConfig.EmbeddingConfig == nil {
		return SearchEvalRunOptions{}, false
	}
	baseURL := strings.TrimRight(strings.TrimSpace(appConfig.EmbeddingConfig.BaseURL), "/")
	model := strings.TrimSpace(appConfig.EmbeddingConfig.Model)
	apiKey := strings.TrimSpace(appConfig.EmbeddingConfig.APIKey)
	timeoutSeconds := appConfig.EmbeddingConfig.TimeoutSeconds
	if baseURL == "" || model == "" {
		return SearchEvalRunOptions{}, false
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	return SearchEvalRunOptions{
		EmbeddingBaseURL:        baseURL,
		EmbeddingAPIKey:         apiKey,
		EmbeddingModel:          model,
		EmbeddingTimeoutSeconds: timeoutSeconds,
	}, true
}

func searchEvalTestConfigPath(root string) string {
	configPath := strings.TrimSpace(os.Getenv("SEARCH_EVAL_CONFIG_PATH"))
	if configPath != "" {
		return configPath
	}
	defaultPath := filepath.Join(root, "config.json")
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}
	return ""
}

// Example_searchEvalCommand 给出运行方式，方便维护者直接执行效果回归测试。
func Example_searchEvalCommand() {
	fmt.Println("go test ./internal/memory -run TestSearchEvalDataset -v")
	// Output:
	// go test ./internal/memory -run TestSearchEvalDataset -v
}
