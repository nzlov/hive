package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/memory"
)

// main 负责加载离线评测数据、执行多参数实验并输出报告文件。
func main() {
	root := flag.String("root", ".", "仓库根目录")
	reportDir := flag.String("report-dir", "", "评测报告输出目录")
	configPath := flag.String("config-path", "", "必填，指定包含 embedding 配置的配置文件路径")
	writeJSONReport := flag.Bool("write-json-report", true, "是否输出 JSON 格式评测报告")
	writeMarkdownReport := flag.Bool("write-markdown-report", true, "是否输出 Markdown 格式评测报告")
	flag.Parse()

	cleanRoot, err := filepath.Abs(*root)
	if err != nil {
		log.Fatalf("解析根目录失败: %v", err)
	}
	dataset, experimentFile, err := memory.LoadSearchEvalDataset(cleanRoot)
	if err != nil {
		log.Fatalf("加载评测数据失败: %v", err)
	}
	if strings.TrimSpace(*configPath) == "" {
		log.Fatalf("必须传入 --config-path")
	}
	resolvedConfigPath, err := filepath.Abs(*configPath)
	if err != nil {
		log.Fatalf("解析 config-path 失败: %v", err)
	}
	projectConfig, err := config.LoadFromPath(resolvedConfigPath)
	if err != nil {
		log.Fatalf("加载指定配置文件失败: %v", err)
	}
	runOptions, err := loadEmbeddingOptionsFromConfig(projectConfig)
	if err != nil {
		log.Fatalf("加载指定配置文件中的 embedding 配置失败: %v", err)
	}
	experimentFile.Baseline = memory.BuildSearchEvalScenarioFromAppConfig(projectConfig)
	suiteReport, err := memory.RunSearchEvalSuiteWithOptions(context.Background(), dataset, experimentFile, runOptions)
	if err != nil {
		log.Fatalf("执行评测实验失败: %v", err)
	}
	outputDir := *reportDir
	if outputDir == "" {
		outputDir = filepath.Join(cleanRoot, "testdata", "search_eval", "reports")
	}
	jsonPath := ""
	markdownPath := ""
	if *writeJSONReport {
		jsonPath, err = memory.WriteSearchEvalJSONReport(outputDir, suiteReport)
		if err != nil {
			log.Fatalf("写入 JSON 评测报告失败: %v", err)
		}
	}
	if *writeMarkdownReport {
		markdownPath, err = memory.WriteSearchEvalMarkdownReport(outputDir, suiteReport)
		if err != nil {
			log.Fatalf("写入 Markdown 评测报告失败: %v", err)
		}
	}
	fmt.Printf("embedding config source: %s\n", resolvedConfigPath)
	fmt.Printf("embedding provider: external baseURL=%s model=%s\n", runOptions.EmbeddingBaseURL, runOptions.EmbeddingModel)
	fmt.Printf("baseline Recall@5=%.4f MRR@10=%.4f ErrorRecall@5=%.4f P95LatencyMS=%.3f\n", suiteReport.Baseline.Summary.RecallAt5, suiteReport.Baseline.Summary.MRRAt10, suiteReport.Baseline.Summary.ErrorRecallAt5, suiteReport.Baseline.Summary.P95LatencyMS)
	fmt.Printf("recommended experiment: %s score=%.4f\n", suiteReport.Recommendation.ExperimentName, suiteReport.Recommendation.Score)
	if len(suiteReport.Recommendation.AppliedExperiments) > 0 {
		fmt.Printf("tuning path: %s\n", strings.Join(suiteReport.Recommendation.AppliedExperiments, " -> "))
	}
	if len(suiteReport.Recommendation.ConfigPatch) > 0 {
		patchBytes, err := prettyJSON(suiteReport.Recommendation.ConfigPatch)
		if err == nil {
			fmt.Printf("recommended config patch:\n%s\n", patchBytes)
		}
	}
	if jsonPath != "" {
		fmt.Printf("JSON report: %s\n", jsonPath)
	} else {
		fmt.Println("JSON report: disabled")
	}
	if markdownPath != "" {
		fmt.Printf("Markdown report: %s\n", markdownPath)
	} else {
		fmt.Println("Markdown report: disabled")
	}
	for _, experiment := range suiteReport.Experiments {
		status := "PASS"
		if !experiment.Passed {
			status = "FAIL"
		}
		fmt.Printf("%s %s Recall@5=%.4f (%+.4f) MRR@10=%.4f (%+.4f) ErrorRecall@5=%.4f (%+.4f) P95LatencyMS=%.3f (%+.3f)\n", experiment.Name, status, experiment.Summary.RecallAt5, experiment.Delta.RecallAt5, experiment.Summary.MRRAt10, experiment.Delta.MRRAt10, experiment.Summary.ErrorRecallAt5, experiment.Delta.ErrorRecallAt5, experiment.Summary.P95LatencyMS, experiment.Delta.P95LatencyMS)
	}
}

// loadEmbeddingOptionsFromConfig 直接复用已解析配置中的 embedding 字段，避免命令层再维护一份默认值规则。
func loadEmbeddingOptionsFromConfig(appConfig config.AppConfig) (memory.SearchEvalRunOptions, error) {
	if appConfig.EmbeddingConfig == nil {
		return memory.SearchEvalRunOptions{}, fmt.Errorf("配置缺少 embedding 段")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(appConfig.EmbeddingConfig.BaseURL), "/")
	apiKey := strings.TrimSpace(appConfig.EmbeddingConfig.APIKey)
	model := strings.TrimSpace(appConfig.EmbeddingConfig.Model)
	timeoutSeconds := appConfig.EmbeddingConfig.TimeoutSeconds
	if baseURL == "" || model == "" {
		return memory.SearchEvalRunOptions{}, fmt.Errorf("配置缺少 embedding.baseUrl 或 embedding.model")
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	return memory.SearchEvalRunOptions{
		EmbeddingBaseURL:        baseURL,
		EmbeddingAPIKey:         apiKey,
		EmbeddingModel:          model,
		EmbeddingTimeoutSeconds: timeoutSeconds,
	}, nil
}

// prettyJSON 统一格式化推荐配置片段，便于命令行直接复制到 config.json。
func prettyJSON(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
