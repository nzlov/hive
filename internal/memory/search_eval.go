package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nzlov/hive/internal/config"
	"github.com/nzlov/hive/internal/models"
)

// SearchEvalCorpusRecord 描述评测语料行，便于把测试数据稳定导入临时数据库。
type SearchEvalCorpusRecord struct {
	ID          string   `json:"id"`
	ProjectName string   `json:"project_name"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Tags        []string `json:"tags"`
	Summary     string   `json:"summary"`
	Content     string   `json:"content"`
	Timestamp   string   `json:"timestamp"`
}

// SearchEvalQueryRecord 描述单条查询配置，便于评测时按类别聚合指标。
type SearchEvalQueryRecord struct {
	QueryID     string   `json:"query_id"`
	ProjectName string   `json:"project_name"`
	Queries     []string `json:"queries"`
	SourceTypes []string `json:"source_types"`
	Category    string   `json:"category"`
	Difficulty  string   `json:"difficulty"`
}

// SearchEvalQrelsRecord 描述查询与期望命中的映射，用于计算召回和排序指标。
type SearchEvalQrelsRecord struct {
	QueryID         string         `json:"query_id"`
	ExpectedTop1    []string       `json:"expected_top1"`
	RelevantIDs     []string       `json:"relevant_ids"`
	GradedRelevance map[string]int `json:"graded_relevance"`
}

// SearchEvalAcceptance 保存最低验收门槛，避免测试和报告把阈值写死两份。
type SearchEvalAcceptance struct {
	RecallAt5                float64 `json:"recall_at_5"`
	MRRAt10                  float64 `json:"mrr_at_10"`
	ErrorRecallAt5           float64 `json:"error_recall_at_5"`
	MaxP95LatencyRatioVsBase float64 `json:"max_p95_latency_ratio_vs_baseline"`
}

// SearchEvalScenarioConfig 描述一次实验配置，便于批量覆盖基线参数。
type SearchEvalScenarioConfig struct {
	Name      string                     `json:"name,omitempty"`
	Embedding *SearchEvalEmbeddingConfig `json:"embedding,omitempty"`
	Search    *SearchEvalSearchConfig    `json:"search,omitempty"`
	AppConfig *config.AppConfig          `json:"-"`
}

// SearchEvalEmbeddingConfig 描述评测需要覆盖的嵌入相关配置。
type SearchEvalEmbeddingConfig struct {
	BaseURL                     *string                      `json:"baseUrl,omitempty"`
	APIKey                      *string                      `json:"apiKey,omitempty"`
	Model                       *string                      `json:"model,omitempty"`
	TimeoutSeconds              *float64                     `json:"timeoutSeconds,omitempty"`
	SemanticSimilarityThreshold *float64                     `json:"semanticSimilarityThreshold,omitempty"`
	SemanticHitFetchLimit       *int                         `json:"semanticHitFetchLimit,omitempty"`
	SemanticWindow              *SearchEvalSemanticWindowCfg `json:"semanticWindow,omitempty"`
	Decay                       *SearchEvalDecayCfg          `json:"decay,omitempty"`
}

// SearchEvalSemanticWindowCfg 描述语义窗口覆盖项，避免窗口实验写死在代码里。
type SearchEvalSemanticWindowCfg struct {
	Mode            *string  `json:"mode,omitempty"`
	BaseMaxCount    *int     `json:"baseMaxCount,omitempty"`
	DynamicMinCount *int     `json:"dynamicMinCount,omitempty"`
	DynamicMaxCount *int     `json:"dynamicMaxCount,omitempty"`
	DynamicRatio    *float64 `json:"dynamicRatio,omitempty"`
}

// SearchEvalDecayCfg 描述时间衰减覆盖项，便于验证旧记忆排序策略。
type SearchEvalDecayCfg struct {
	Enabled        *bool                      `json:"enabled,omitempty"`
	AgeWeight      *float64                   `json:"ageWeight,omitempty"`
	SemanticWeight *float64                   `json:"semanticWeight,omitempty"`
	HalfLifeDays   *SearchEvalHalfLifeDaysCfg `json:"halfLifeDays,omitempty"`
}

// SearchEvalHalfLifeDaysCfg 描述不同记忆类型的半衰期配置。
type SearchEvalHalfLifeDaysCfg struct {
	Summary *float64 `json:"summary,omitempty"`
	Error   *float64 `json:"error,omitempty"`
}

// SearchEvalSearchConfig 描述搜索融合与关键字相关覆盖项。
type SearchEvalSearchConfig struct {
	Keyword *SearchEvalKeywordCfg `json:"keyword,omitempty"`
	Fusion  *SearchEvalFusionCfg  `json:"fusion,omitempty"`
}

// SearchEvalKeywordCfg 描述关键字模式覆盖项。
type SearchEvalKeywordCfg struct {
	Mode *string `json:"mode,omitempty"`
}

// SearchEvalFusionCfg 描述融合配置覆盖项，便于批量实验排序策略。
type SearchEvalFusionCfg struct {
	Enabled              *bool    `json:"enabled,omitempty"`
	Formula              *string  `json:"formula,omitempty"`
	KeywordWeight        *float64 `json:"keywordWeight,omitempty"`
	SemanticWeight       *float64 `json:"semanticWeight,omitempty"`
	RecencyWeight        *float64 `json:"recencyWeight,omitempty"`
	MinSemanticScore     *float64 `json:"minSemanticScore,omitempty"`
	CoverageDiscountBase *float64 `json:"coverageDiscountBase,omitempty"`
}

// SearchEvalExperimentFile 保存实验矩阵和验收规则，便于 CLI 与测试共用同一份定义。
type SearchEvalExperimentFile struct {
	Baseline       SearchEvalScenarioConfig   `json:"baseline"`
	ThresholdSweep []SearchEvalScenarioConfig `json:"threshold_sweep"`
	KeywordModes   []SearchEvalScenarioConfig `json:"keyword_modes"`
	FusionSets     []SearchEvalScenarioConfig `json:"fusion_sets"`
	DecaySets      []SearchEvalScenarioConfig `json:"decay_sets"`
	WindowSets     []SearchEvalScenarioConfig `json:"window_sets"`
	Acceptance     SearchEvalAcceptance       `json:"acceptance"`
}

// SearchEvalDataset 保存离线评测所需的全部数据文件内容。
type SearchEvalDataset struct {
	Root    string
	Corpus  []SearchEvalCorpusRecord
	Queries []SearchEvalQueryRecord
	Qrels   map[string]SearchEvalQrelsRecord
}

// SearchEvalResult 保存单条查询的命中明细与耗时，方便输出失败样本和聚合指标。
type SearchEvalResult struct {
	Query       SearchEvalQueryRecord `json:"query"`
	Ranking     []string              `json:"ranking"`
	LatencyMS   float64               `json:"latency_ms"`
	RecallAt5   float64               `json:"recall_at_5"`
	MRRAt10     float64               `json:"mrr_at_10"`
	NDCGAt10    float64               `json:"ndcg_at_10"`
	Top1Correct bool                  `json:"top1_correct"`
}

// SearchEvalSummary 汇总整套数据集的核心指标，便于断言和实验对比。
type SearchEvalSummary struct {
	RecallAt5      float64 `json:"recall_at_5"`
	MRRAt10        float64 `json:"mrr_at_10"`
	NDCGAt10       float64 `json:"ndcg_at_10"`
	Top1Accuracy   float64 `json:"top1_accuracy"`
	ErrorRecallAt5 float64 `json:"error_recall_at_5"`
	P95LatencyMS   float64 `json:"p95_latency_ms"`
}

// SearchEvalSummaryDelta 保存相对 baseline 的指标变化，便于快速判断调参收益和代价。
type SearchEvalSummaryDelta struct {
	RecallAt5      float64 `json:"recall_at_5"`
	MRRAt10        float64 `json:"mrr_at_10"`
	NDCGAt10       float64 `json:"ndcg_at_10"`
	Top1Accuracy   float64 `json:"top1_accuracy"`
	ErrorRecallAt5 float64 `json:"error_recall_at_5"`
	P95LatencyMS   float64 `json:"p95_latency_ms"`
}

// SearchEvalFailure 保存失败样本摘要，便于 Markdown 报告快速复盘。
type SearchEvalFailure struct {
	QueryID      string   `json:"query_id"`
	Category     string   `json:"category"`
	ExpectedTop1 []string `json:"expected_top1"`
	ActualTop5   []string `json:"actual_top5"`
	RecallAt5    float64  `json:"recall_at_5"`
	MRRAt10      float64  `json:"mrr_at_10"`
	Top1Correct  bool     `json:"top1_correct"`
}

// SearchEvalExperimentReport 保存单组实验的详细结果和通过状态。
type SearchEvalExperimentReport struct {
	Name            string                 `json:"name"`
	Config          config.AppConfig       `json:"-"`
	Summary         SearchEvalSummary      `json:"summary"`
	Delta           SearchEvalSummaryDelta `json:"delta"`
	Results         []SearchEvalResult     `json:"results"`
	Failures        []SearchEvalFailure    `json:"failures"`
	Passed          bool                   `json:"passed"`
	ValidationError []string               `json:"validation_errors,omitempty"`
}

// SearchEvalSuiteReport 保存多参数实验总结果，便于输出 JSON 和 Markdown 报告。
type SearchEvalSuiteReport struct {
	GeneratedAt    string                       `json:"generated_at"`
	Embedding      SearchEvalEmbeddingRuntime   `json:"embedding"`
	Acceptance     SearchEvalAcceptance         `json:"acceptance"`
	Baseline       SearchEvalExperimentReport   `json:"baseline"`
	Experiments    []SearchEvalExperimentReport `json:"experiments"`
	Recommendation SearchEvalRecommendation     `json:"recommendation"`
}

// SearchEvalEmbeddingRuntime 描述本次评测实际使用的向量提供方式，便于报告和结果追溯。
type SearchEvalEmbeddingRuntime struct {
	Mode           string  `json:"mode"`
	BaseURL        string  `json:"base_url,omitempty"`
	Model          string  `json:"model,omitempty"`
	TimeoutSeconds float64 `json:"timeout_seconds,omitempty"`
}

// SearchEvalRunOptions 描述评测运行时的附加选项，便于命令行覆盖外部向量服务配置。
type SearchEvalRunOptions struct {
	EmbeddingBaseURL        string
	EmbeddingAPIKey         string
	EmbeddingModel          string
	EmbeddingTimeoutSeconds float64
}

// SearchEvalRecommendation 保存自动调优后选出的最优方案与配置差异，便于直接落回项目配置。
type SearchEvalRecommendation struct {
	ExperimentName     string                 `json:"experiment_name"`
	AppliedExperiments []string               `json:"applied_experiments,omitempty"`
	Summary            SearchEvalSummary      `json:"summary"`
	Delta              SearchEvalSummaryDelta `json:"delta"`
	Score              float64                `json:"score"`
	Reasons            []string               `json:"reasons"`
	ConfigPatch        map[string]any         `json:"config_patch"`
}

type searchEvalStage struct {
	name      string
	scenarios []SearchEvalScenarioConfig
}

const searchEvalMaxTuningPasses = 5

// searchEvalExternalEmbeddingProvider 直接复用真实嵌入服务，确保评测结果来自外部模型而非本地伪向量。
type searchEvalExternalEmbeddingProvider struct {
	delegate EmbeddingProvider
}

// Enabled 直接复用真实嵌入服务启用状态，避免评测命令和服务配置出现双重判断。
func (p searchEvalExternalEmbeddingProvider) Enabled() bool {
	return p.delegate != nil && p.delegate.Enabled()
}

// ModelName 直接透传真实模型名，便于报告记录最终使用的外部模型。
func (p searchEvalExternalEmbeddingProvider) ModelName() string {
	if p.delegate == nil {
		return ""
	}
	return p.delegate.ModelName()
}

// EmbedTexts 直接把文本交给真实嵌入服务，确保语义评测忠实反映外部模型表现。
func (p searchEvalExternalEmbeddingProvider) EmbedTexts(texts []string) ([][]float64, error) {
	if p.delegate == nil {
		return nil, nil
	}
	return p.delegate.EmbedTexts(texts)
}

// LoadSearchEvalDataset 读取 testdata/search_eval 下的数据集和实验配置。
func LoadSearchEvalDataset(root string) (SearchEvalDataset, SearchEvalExperimentFile, error) {
	cleanRoot := filepath.Clean(root)
	datasetDir := filepath.Join(cleanRoot, "testdata", "search_eval")
	corpus, err := loadSearchEvalCorpusFile(filepath.Join(datasetDir, "corpus.jsonl"))
	if err != nil {
		return SearchEvalDataset{}, SearchEvalExperimentFile{}, err
	}
	queries, err := loadSearchEvalQueriesFile(filepath.Join(datasetDir, "queries.jsonl"))
	if err != nil {
		return SearchEvalDataset{}, SearchEvalExperimentFile{}, err
	}
	qrels, err := loadSearchEvalQrelsFile(filepath.Join(datasetDir, "qrels.jsonl"))
	if err != nil {
		return SearchEvalDataset{}, SearchEvalExperimentFile{}, err
	}
	experimentFile, err := loadSearchEvalExperimentFile(filepath.Join(datasetDir, "experiment_sets.json"))
	if err != nil {
		return SearchEvalDataset{}, SearchEvalExperimentFile{}, err
	}
	return SearchEvalDataset{Root: cleanRoot, Corpus: corpus, Queries: queries, Qrels: qrels}, experimentFile, nil
}

// BuildSearchEvalAppConfig 根据场景配置生成真正用于搜索的服务配置。
func BuildSearchEvalAppConfig(memoryRoot string, scenario SearchEvalScenarioConfig) config.AppConfig {
	if scenario.AppConfig != nil {
		appConfig := cloneSearchEvalAppConfig(*scenario.AppConfig)
		appConfig.MemoryRoot = memoryRoot
		applySearchEvalScenarioConfig(&appConfig, scenario)
		return appConfig
	}
	appConfig := config.AppConfig{
		MemoryRoot: memoryRoot,
		EmbeddingConfig: &config.EmbeddingConfig{
			SemanticSimilarityThreshold: 0.15,
			SemanticCandidateBatchSize:  256,
			SemanticCandidateMaxCount:   1024,
			SemanticHitFetchLimit:       64,
			SemanticWindowMode:          "static",
			SemanticWindowBaseMaxCount:  1024,
			SemanticWindowDynamicMin:    256,
			SemanticWindowDynamicMax:    20000,
			SemanticWindowDynamicRatio:  0.2,
			DecayEnabled:                true,
			DecayAgeWeight:              0.5,
			DecaySemanticWeight:         0.5,
			DecaySummaryHalfLifeDays:    30,
			DecayErrorHalfLifeDays:      90,
		},
		SearchConfig: &config.SearchConfig{
			LowConfidenceErrorHitLimit:   10,
			LowConfidenceSummaryHitLimit: 10,
			KeywordMode:                  "like",
			FusionEnabled:                true,
			FusionFormula:                "coverage_discount",
			FusionKeywordWeight:          0.55,
			FusionSemanticWeight:         0.45,
			FusionRecencyWeight:          0.10,
			FusionCoverageDiscountBase:   0.85,
			KeywordSynonymsEnabled:       false,
		},
	}
	applySearchEvalScenarioConfig(&appConfig, scenario)
	return appConfig
}

// BuildSearchEvalScenarioFromAppConfig 把项目当前配置映射成评测基线，确保自动调优以真实默认参数为起点。
func BuildSearchEvalScenarioFromAppConfig(appConfig config.AppConfig) SearchEvalScenarioConfig {
	scenario := SearchEvalScenarioConfig{Name: "current-default", AppConfig: cloneSearchEvalAppConfigPtr(appConfig)}
	if appConfig.EmbeddingConfig != nil {
		scenario.Embedding = &SearchEvalEmbeddingConfig{
			BaseURL:                     stringPtr(appConfig.EmbeddingConfig.BaseURL),
			APIKey:                      stringPtr(appConfig.EmbeddingConfig.APIKey),
			Model:                       stringPtr(appConfig.EmbeddingConfig.Model),
			TimeoutSeconds:              float64Ptr(appConfig.EmbeddingConfig.TimeoutSeconds),
			SemanticSimilarityThreshold: float64Ptr(appConfig.EmbeddingConfig.SemanticSimilarityThreshold),
			SemanticHitFetchLimit:       intPtr(appConfig.EmbeddingConfig.SemanticHitFetchLimit),
			SemanticWindow: &SearchEvalSemanticWindowCfg{
				Mode:            stringPtr(appConfig.EmbeddingConfig.SemanticWindowMode),
				BaseMaxCount:    intPtr(appConfig.EmbeddingConfig.SemanticWindowBaseMaxCount),
				DynamicMinCount: intPtr(appConfig.EmbeddingConfig.SemanticWindowDynamicMin),
				DynamicMaxCount: intPtr(appConfig.EmbeddingConfig.SemanticWindowDynamicMax),
				DynamicRatio:    float64Ptr(appConfig.EmbeddingConfig.SemanticWindowDynamicRatio),
			},
			Decay: &SearchEvalDecayCfg{
				Enabled:        boolPtr(appConfig.EmbeddingConfig.DecayEnabled),
				AgeWeight:      float64Ptr(appConfig.EmbeddingConfig.DecayAgeWeight),
				SemanticWeight: float64Ptr(appConfig.EmbeddingConfig.DecaySemanticWeight),
				HalfLifeDays: &SearchEvalHalfLifeDaysCfg{
					Summary: float64Ptr(appConfig.EmbeddingConfig.DecaySummaryHalfLifeDays),
					Error:   float64Ptr(appConfig.EmbeddingConfig.DecayErrorHalfLifeDays),
				},
			},
		}
	}
	if appConfig.SearchConfig != nil {
		scenario.Search = &SearchEvalSearchConfig{
			Keyword: &SearchEvalKeywordCfg{Mode: stringPtr(appConfig.SearchConfig.KeywordMode)},
			Fusion: &SearchEvalFusionCfg{
				Enabled:              boolPtr(appConfig.SearchConfig.FusionEnabled),
				Formula:              stringPtr(appConfig.SearchConfig.FusionFormula),
				KeywordWeight:        float64Ptr(appConfig.SearchConfig.FusionKeywordWeight),
				SemanticWeight:       float64Ptr(appConfig.SearchConfig.FusionSemanticWeight),
				RecencyWeight:        float64Ptr(appConfig.SearchConfig.FusionRecencyWeight),
				MinSemanticScore:     float64Ptr(appConfig.SearchConfig.FusionMinSemanticScore),
				CoverageDiscountBase: float64Ptr(appConfig.SearchConfig.FusionCoverageDiscountBase),
			},
		}
	}
	return scenario
}

// UseExternalEmbedding 判断是否显式指定了外部向量服务，避免只给半套参数时误启用远端调用。
func (o SearchEvalRunOptions) UseExternalEmbedding() bool {
	return strings.TrimSpace(o.EmbeddingBaseURL) != "" && strings.TrimSpace(o.EmbeddingModel) != ""
}

// RunSearchEvalSuite 依次执行 baseline 和 experiment_sets 中定义的全部实验组合。
func RunSearchEvalSuite(ctx context.Context, dataset SearchEvalDataset, experimentFile SearchEvalExperimentFile) (SearchEvalSuiteReport, error) {
	return RunSearchEvalSuiteWithOptions(ctx, dataset, experimentFile, SearchEvalRunOptions{})
}

// RunSearchEvalSuiteWithOptions 允许评测命令覆盖外部向量服务配置，同时保留基线实验流程不变。
func RunSearchEvalSuiteWithOptions(ctx context.Context, dataset SearchEvalDataset, experimentFile SearchEvalExperimentFile, options SearchEvalRunOptions) (SearchEvalSuiteReport, error) {
	baselineReport, err := RunSearchEvalExperimentWithOptions(ctx, dataset, "baseline", experimentFile.Baseline, options)
	if err != nil {
		return SearchEvalSuiteReport{}, err
	}
	baselineReport.Delta = diffSearchEvalSummary(baselineReport.Summary, baselineReport.Summary)
	baselineReport.Passed, baselineReport.ValidationError = validateSearchEvalReport(baselineReport.Summary, baselineReport.Summary, experimentFile.Acceptance)
	currentScenario := experimentFile.Baseline
	currentReport := baselineReport
	appliedExperiments := []string{}
	experiments := []SearchEvalExperimentReport{}
	for pass := 1; pass <= searchEvalMaxTuningPasses; pass++ {
		passImproved := false
		for _, stage := range buildSearchEvalStages(experimentFile) {
			stageBestScenario := currentScenario
			stageBestReport := currentReport
			stageBestScore := scoreSearchEvalExperiment(currentReport)
			stageSelected := ""
			for _, scenario := range stage.scenarios {
				candidateScenario := mergeSearchEvalScenarioConfig(currentScenario, scenario)
				report, innerErr := RunSearchEvalExperimentWithOptions(ctx, dataset, fmt.Sprintf("pass_%d_%s", pass, scenario.Name), candidateScenario, options)
				if innerErr != nil {
					return SearchEvalSuiteReport{}, innerErr
				}
				report.Delta = diffSearchEvalSummary(report.Summary, baselineReport.Summary)
				report.Passed, report.ValidationError = validateSearchEvalReport(report.Summary, baselineReport.Summary, experimentFile.Acceptance)
				experiments = append(experiments, report)
				score := scoreSearchEvalExperiment(report)
				if score > stageBestScore+1e-9 {
					stageBestScenario = candidateScenario
					stageBestReport = report
					stageBestScore = score
					stageSelected = scenario.Name
				}
			}
			if stageSelected != "" {
				currentScenario = stageBestScenario
				currentReport = stageBestReport
				appliedExperiments = append(appliedExperiments, fmt.Sprintf("pass%d/%s:%s", pass, stage.name, stageSelected))
				passImproved = true
			}
		}
		if !passImproved {
			break
		}
	}
	recommendation := buildSearchEvalRecommendation(baselineReport, currentReport, appliedExperiments)
	return SearchEvalSuiteReport{
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		Embedding:      buildSearchEvalEmbeddingRuntime(options),
		Acceptance:     experimentFile.Acceptance,
		Baseline:       baselineReport,
		Experiments:    experiments,
		Recommendation: recommendation,
	}, nil
}

// RunSearchEvalExperiment 使用指定配置跑一轮完整离线评测，并返回详细结果。
func RunSearchEvalExperiment(ctx context.Context, dataset SearchEvalDataset, name string, scenario SearchEvalScenarioConfig) (SearchEvalExperimentReport, error) {
	return RunSearchEvalExperimentWithOptions(ctx, dataset, name, scenario, SearchEvalRunOptions{})
}

// RunSearchEvalExperimentWithOptions 允许在单次实验里注入外部向量服务，便于命令行直连真实模型。
func RunSearchEvalExperimentWithOptions(ctx context.Context, dataset SearchEvalDataset, name string, scenario SearchEvalScenarioConfig, options SearchEvalRunOptions) (SearchEvalExperimentReport, error) {
	memoryRoot, err := os.MkdirTemp("", "search-eval-*")
	if err != nil {
		return SearchEvalExperimentReport{}, fmt.Errorf("创建评测目录失败: %w", err)
	}
	defer os.RemoveAll(memoryRoot)
	appConfig := BuildSearchEvalAppConfig(memoryRoot, scenario)
	applySearchEvalRunOptions(&appConfig, options)
	store, err := models.Open(appConfig)
	if err != nil {
		return SearchEvalExperimentReport{}, fmt.Errorf("打开评测数据库失败: %w", err)
	}
	defer store.Close()
	service := NewService(appConfig)
	provider, err := buildSearchEvalEmbeddingProvider(appConfig.EmbeddingConfig, options)
	if err != nil {
		return SearchEvalExperimentReport{}, err
	}
	service.provider = provider
	searchCtx := models.StoreToContext(ctx, store)
	idMap, err := seedSearchEvalCorpus(store, service.provider, dataset.Corpus)
	if err != nil {
		return SearchEvalExperimentReport{}, err
	}
	results, err := runSearchEvalQueries(searchCtx, service, dataset.Queries, dataset.Qrels, idMap)
	if err != nil {
		return SearchEvalExperimentReport{}, err
	}
	summary := summarizeSearchEvalResults(results)
	failures := buildSearchEvalFailures(results, dataset.Qrels)
	return SearchEvalExperimentReport{Name: name, Config: appConfig, Summary: summary, Results: results, Failures: failures}, nil
}

// WriteSearchEvalReports 同时输出 JSON 和 Markdown 报告，便于机器消费和人工阅读。
func WriteSearchEvalReports(reportDir string, report SearchEvalSuiteReport) (string, string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", "", fmt.Errorf("创建评测报告目录失败: %w", err)
	}
	jsonPath, err := WriteSearchEvalJSONReport(reportDir, report)
	if err != nil {
		return "", "", err
	}
	markdownPath, err := WriteSearchEvalMarkdownReport(reportDir, report)
	if err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

// WriteSearchEvalJSONReport 单独输出 JSON 报告，便于命令层按需控制机器可读产物。
func WriteSearchEvalJSONReport(reportDir string, report SearchEvalSuiteReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("创建评测报告目录失败: %w", err)
	}
	jsonPath := filepath.Join(reportDir, "search_eval_report.json")
	jsonBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化 JSON 报告失败: %w", err)
	}
	if err := os.WriteFile(jsonPath, jsonBytes, 0o644); err != nil {
		return "", fmt.Errorf("写入 JSON 报告失败: %w", err)
	}
	return jsonPath, nil
}

// WriteSearchEvalMarkdownReport 单独输出 Markdown 报告，便于命令层按需控制人读报告产物。
func WriteSearchEvalMarkdownReport(reportDir string, report SearchEvalSuiteReport) (string, error) {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("创建评测报告目录失败: %w", err)
	}
	markdownPath := filepath.Join(reportDir, "search_eval_report.md")
	markdown := renderSearchEvalMarkdown(report)
	if err := os.WriteFile(markdownPath, []byte(markdown), 0o644); err != nil {
		return "", fmt.Errorf("写入 Markdown 报告失败: %w", err)
	}
	return markdownPath, nil
}

// buildSearchEvalRecommendation 根据分阶段调优后的最终方案生成推荐结论与配置差异。
func buildSearchEvalRecommendation(baseline, best SearchEvalExperimentReport, appliedExperiments []string) SearchEvalRecommendation {
	bestScore := scoreSearchEvalExperiment(best)
	configPatch := buildSearchEvalConfigPatch(baseline.Config, best.Config)
	reasons := buildSearchEvalRecommendationReasons(baseline, best)
	return SearchEvalRecommendation{
		ExperimentName:     best.Name,
		AppliedExperiments: append([]string(nil), appliedExperiments...),
		Summary:            best.Summary,
		Delta:              diffSearchEvalSummary(best.Summary, baseline.Summary),
		Score:              bestScore,
		Reasons:            reasons,
		ConfigPatch:        configPatch,
	}
}

func buildSearchEvalStages(file SearchEvalExperimentFile) []searchEvalStage {
	stages := []searchEvalStage{}
	if len(file.ThresholdSweep) > 0 {
		stages = append(stages, searchEvalStage{name: "threshold", scenarios: file.ThresholdSweep})
	}
	if len(file.KeywordModes) > 0 {
		stages = append(stages, searchEvalStage{name: "keyword", scenarios: file.KeywordModes})
	}
	if len(file.FusionSets) > 0 {
		stages = append(stages, searchEvalStage{name: "fusion", scenarios: file.FusionSets})
	}
	if len(file.DecaySets) > 0 {
		stages = append(stages, searchEvalStage{name: "decay", scenarios: file.DecaySets})
	}
	if len(file.WindowSets) > 0 {
		stages = append(stages, searchEvalStage{name: "window", scenarios: file.WindowSets})
	}
	return stages
}

// scoreSearchEvalExperiment 用召回、排序与延迟的组合分来选最优实验，避免只追单一指标。
func scoreSearchEvalExperiment(report SearchEvalExperimentReport) float64 {
	return report.Summary.ErrorRecallAt5*0.4 + report.Summary.MRRAt10*0.25 + report.Summary.RecallAt5*0.2 + report.Summary.NDCGAt10*0.1 + report.Summary.Top1Accuracy*0.05 - report.Summary.P95LatencyMS/10000
}

// buildSearchEvalRecommendationReasons 解释为什么推荐该方案，方便把结果同步给项目维护者。
func buildSearchEvalRecommendationReasons(baseline, best SearchEvalExperimentReport) []string {
	reasons := []string{}
	delta := diffSearchEvalSummary(best.Summary, baseline.Summary)
	if best.Name == baseline.Name {
		return []string{"当前默认参数已经是本轮评测中的最优方案，无需调整。"}
	}
	if delta.ErrorRecallAt5 > 0 {
		reasons = append(reasons, fmt.Sprintf("错误类 Recall@5 提升 %.4f，更适合故障定位与历史错误复用。", delta.ErrorRecallAt5))
	}
	if delta.MRRAt10 > 0 {
		reasons = append(reasons, fmt.Sprintf("MRR@10 提升 %.4f，说明更相关的答案更稳定地排到前面。", delta.MRRAt10))
	}
	if delta.RecallAt5 > 0 {
		reasons = append(reasons, fmt.Sprintf("Recall@5 提升 %.4f，整体召回覆盖更好。", delta.RecallAt5))
	}
	if delta.P95LatencyMS < 0 {
		reasons = append(reasons, fmt.Sprintf("P95 延迟下降 %.3fms，调优后没有引入额外响应成本。", -delta.P95LatencyMS))
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "该方案在综合评分函数下优于当前默认参数。")
	}
	return reasons
}

// buildSearchEvalConfigPatch 只输出与当前默认参数不同的配置片段，便于直接合入 config.json。
func buildSearchEvalConfigPatch(baseline, best config.AppConfig) map[string]any {
	patch := map[string]any{}
	if embeddingPatch := buildSearchEvalEmbeddingPatch(baseline.EmbeddingConfig, best.EmbeddingConfig); len(embeddingPatch) > 0 {
		patch["embedding"] = embeddingPatch
	}
	if searchPatch := buildSearchEvalSearchPatch(baseline.SearchConfig, best.SearchConfig); len(searchPatch) > 0 {
		patch["search"] = searchPatch
	}
	return patch
}

func buildSearchEvalEmbeddingPatch(base, best *config.EmbeddingConfig) map[string]any {
	if base == nil || best == nil {
		return nil
	}
	patch := map[string]any{}
	if base.SemanticSimilarityThreshold != best.SemanticSimilarityThreshold {
		patch["semanticSimilarityThreshold"] = best.SemanticSimilarityThreshold
	}
	if base.SemanticHitFetchLimit != best.SemanticHitFetchLimit {
		patch["semanticHitFetchLimit"] = best.SemanticHitFetchLimit
	}
	windowPatch := map[string]any{}
	if base.SemanticWindowMode != best.SemanticWindowMode {
		windowPatch["mode"] = best.SemanticWindowMode
	}
	if base.SemanticWindowBaseMaxCount != best.SemanticWindowBaseMaxCount {
		windowPatch["baseMaxCount"] = best.SemanticWindowBaseMaxCount
	}
	if base.SemanticWindowDynamicMin != best.SemanticWindowDynamicMin {
		windowPatch["dynamicMinCount"] = best.SemanticWindowDynamicMin
	}
	if base.SemanticWindowDynamicMax != best.SemanticWindowDynamicMax {
		windowPatch["dynamicMaxCount"] = best.SemanticWindowDynamicMax
	}
	if base.SemanticWindowDynamicRatio != best.SemanticWindowDynamicRatio {
		windowPatch["dynamicRatio"] = best.SemanticWindowDynamicRatio
	}
	if len(windowPatch) > 0 {
		patch["semanticWindow"] = windowPatch
	}
	decayPatch := map[string]any{}
	if base.DecayEnabled != best.DecayEnabled {
		decayPatch["enabled"] = best.DecayEnabled
	}
	if base.DecayAgeWeight != best.DecayAgeWeight {
		decayPatch["ageWeight"] = best.DecayAgeWeight
	}
	if base.DecaySemanticWeight != best.DecaySemanticWeight {
		decayPatch["semanticWeight"] = best.DecaySemanticWeight
	}
	halfLifePatch := map[string]any{}
	if base.DecaySummaryHalfLifeDays != best.DecaySummaryHalfLifeDays {
		halfLifePatch["summary"] = best.DecaySummaryHalfLifeDays
	}
	if base.DecayErrorHalfLifeDays != best.DecayErrorHalfLifeDays {
		halfLifePatch["error"] = best.DecayErrorHalfLifeDays
	}
	if len(halfLifePatch) > 0 {
		decayPatch["halfLifeDays"] = halfLifePatch
	}
	if len(decayPatch) > 0 {
		patch["decay"] = decayPatch
	}
	return patch
}

func buildSearchEvalSearchPatch(base, best *config.SearchConfig) map[string]any {
	if base == nil || best == nil {
		return nil
	}
	patch := map[string]any{}
	keywordPatch := map[string]any{}
	if base.KeywordMode != best.KeywordMode {
		keywordPatch["mode"] = best.KeywordMode
	}
	if len(keywordPatch) > 0 {
		patch["keyword"] = keywordPatch
	}
	fusionPatch := map[string]any{}
	if base.FusionEnabled != best.FusionEnabled {
		fusionPatch["enabled"] = best.FusionEnabled
	}
	if base.FusionFormula != best.FusionFormula {
		fusionPatch["formula"] = best.FusionFormula
	}
	if base.FusionKeywordWeight != best.FusionKeywordWeight {
		fusionPatch["keywordWeight"] = best.FusionKeywordWeight
	}
	if base.FusionSemanticWeight != best.FusionSemanticWeight {
		fusionPatch["semanticWeight"] = best.FusionSemanticWeight
	}
	if base.FusionRecencyWeight != best.FusionRecencyWeight {
		fusionPatch["recencyWeight"] = best.FusionRecencyWeight
	}
	if base.FusionMinSemanticScore != best.FusionMinSemanticScore {
		fusionPatch["minSemanticScore"] = best.FusionMinSemanticScore
	}
	if base.FusionCoverageDiscountBase != best.FusionCoverageDiscountBase {
		fusionPatch["coverageDiscountBase"] = best.FusionCoverageDiscountBase
	}
	if len(fusionPatch) > 0 {
		patch["fusion"] = fusionPatch
	}
	return patch
}

// buildSearchEvalEmbeddingRuntime 统一生成报告中的向量服务信息，避免命令层和报告层各自拼装。
func buildSearchEvalEmbeddingRuntime(options SearchEvalRunOptions) SearchEvalEmbeddingRuntime {
	if options.UseExternalEmbedding() {
		return SearchEvalEmbeddingRuntime{
			Mode:           "external",
			BaseURL:        strings.TrimSpace(options.EmbeddingBaseURL),
			Model:          strings.TrimSpace(options.EmbeddingModel),
			TimeoutSeconds: options.EmbeddingTimeoutSeconds,
		}
	}
	return SearchEvalEmbeddingRuntime{Mode: "local-deterministic"}
}

// flattenSearchEvalScenarios 按 experiment_sets 分组顺序展开实验列表，便于稳定输出报告。
func flattenSearchEvalScenarios(file SearchEvalExperimentFile) []SearchEvalScenarioConfig {
	out := []SearchEvalScenarioConfig{}
	out = append(out, file.ThresholdSweep...)
	out = append(out, file.KeywordModes...)
	out = append(out, file.FusionSets...)
	out = append(out, file.DecaySets...)
	out = append(out, file.WindowSets...)
	return out
}

// mergeSearchEvalScenarioConfig 把实验覆盖项叠加到 baseline 上，避免每个实验都重复写全量配置。
func mergeSearchEvalScenarioConfig(base, override SearchEvalScenarioConfig) SearchEvalScenarioConfig {
	merged := SearchEvalScenarioConfig{Name: override.Name}
	if merged.Name == "" {
		merged.Name = base.Name
	}
	if override.AppConfig != nil {
		merged.AppConfig = cloneSearchEvalAppConfigPtr(*override.AppConfig)
	} else if base.AppConfig != nil {
		merged.AppConfig = cloneSearchEvalAppConfigPtr(*base.AppConfig)
	}
	merged.Embedding = mergeSearchEvalEmbeddingConfig(base.Embedding, override.Embedding)
	merged.Search = mergeSearchEvalSearchConfig(base.Search, override.Search)
	return merged
}

// applySearchEvalScenarioConfig 把场景配置映射到真正的应用配置，便于服务层直接复用。
func applySearchEvalScenarioConfig(appConfig *config.AppConfig, scenario SearchEvalScenarioConfig) {
	if appConfig == nil {
		return
	}
	if scenario.Embedding != nil && appConfig.EmbeddingConfig != nil {
		if value := scenario.Embedding.BaseURL; value != nil {
			appConfig.EmbeddingConfig.BaseURL = strings.TrimRight(strings.TrimSpace(*value), "/")
		}
		if value := scenario.Embedding.APIKey; value != nil {
			appConfig.EmbeddingConfig.APIKey = strings.TrimSpace(*value)
		}
		if value := scenario.Embedding.Model; value != nil {
			appConfig.EmbeddingConfig.Model = strings.TrimSpace(*value)
		}
		if value := scenario.Embedding.TimeoutSeconds; value != nil {
			appConfig.EmbeddingConfig.TimeoutSeconds = *value
		}
		if value := scenario.Embedding.SemanticSimilarityThreshold; value != nil {
			appConfig.EmbeddingConfig.SemanticSimilarityThreshold = *value
		}
		if value := scenario.Embedding.SemanticHitFetchLimit; value != nil {
			appConfig.EmbeddingConfig.SemanticHitFetchLimit = *value
		}
		if scenario.Embedding.SemanticWindow != nil {
			if value := scenario.Embedding.SemanticWindow.Mode; value != nil {
				appConfig.EmbeddingConfig.SemanticWindowMode = *value
			}
			if value := scenario.Embedding.SemanticWindow.BaseMaxCount; value != nil {
				appConfig.EmbeddingConfig.SemanticWindowBaseMaxCount = *value
			}
			if value := scenario.Embedding.SemanticWindow.DynamicMinCount; value != nil {
				appConfig.EmbeddingConfig.SemanticWindowDynamicMin = *value
			}
			if value := scenario.Embedding.SemanticWindow.DynamicMaxCount; value != nil {
				appConfig.EmbeddingConfig.SemanticWindowDynamicMax = *value
			}
			if value := scenario.Embedding.SemanticWindow.DynamicRatio; value != nil {
				appConfig.EmbeddingConfig.SemanticWindowDynamicRatio = *value
			}
		}
		if scenario.Embedding.Decay != nil {
			if value := scenario.Embedding.Decay.Enabled; value != nil {
				appConfig.EmbeddingConfig.DecayEnabled = *value
			}
			if value := scenario.Embedding.Decay.AgeWeight; value != nil {
				appConfig.EmbeddingConfig.DecayAgeWeight = *value
			}
			if value := scenario.Embedding.Decay.SemanticWeight; value != nil {
				appConfig.EmbeddingConfig.DecaySemanticWeight = *value
			}
			if scenario.Embedding.Decay.HalfLifeDays != nil {
				if value := scenario.Embedding.Decay.HalfLifeDays.Summary; value != nil {
					appConfig.EmbeddingConfig.DecaySummaryHalfLifeDays = *value
				}
				if value := scenario.Embedding.Decay.HalfLifeDays.Error; value != nil {
					appConfig.EmbeddingConfig.DecayErrorHalfLifeDays = *value
				}
			}
		}
	}
	if scenario.Search != nil && appConfig.SearchConfig != nil {
		if scenario.Search.Keyword != nil {
			if value := scenario.Search.Keyword.Mode; value != nil {
				appConfig.SearchConfig.KeywordMode = *value
			}
		}
		if scenario.Search.Fusion != nil {
			if value := scenario.Search.Fusion.Enabled; value != nil {
				appConfig.SearchConfig.FusionEnabled = *value
			}
			if value := scenario.Search.Fusion.Formula; value != nil {
				appConfig.SearchConfig.FusionFormula = *value
			}
			if value := scenario.Search.Fusion.KeywordWeight; value != nil {
				appConfig.SearchConfig.FusionKeywordWeight = *value
			}
			if value := scenario.Search.Fusion.SemanticWeight; value != nil {
				appConfig.SearchConfig.FusionSemanticWeight = *value
			}
			if value := scenario.Search.Fusion.RecencyWeight; value != nil {
				appConfig.SearchConfig.FusionRecencyWeight = *value
			}
			if value := scenario.Search.Fusion.MinSemanticScore; value != nil {
				appConfig.SearchConfig.FusionMinSemanticScore = *value
			}
			if value := scenario.Search.Fusion.CoverageDiscountBase; value != nil {
				appConfig.SearchConfig.FusionCoverageDiscountBase = *value
			}
		}
	}
}

// applySearchEvalRunOptions 把命令行显式指定的外部嵌入配置覆盖到实验配置，确保所有实验使用同一模型。
func applySearchEvalRunOptions(appConfig *config.AppConfig, options SearchEvalRunOptions) {
	if appConfig == nil || appConfig.EmbeddingConfig == nil || !options.UseExternalEmbedding() {
		return
	}
	appConfig.EmbeddingConfig.BaseURL = strings.TrimRight(strings.TrimSpace(options.EmbeddingBaseURL), "/")
	appConfig.EmbeddingConfig.APIKey = strings.TrimSpace(options.EmbeddingAPIKey)
	appConfig.EmbeddingConfig.Model = strings.TrimSpace(options.EmbeddingModel)
	if options.EmbeddingTimeoutSeconds > 0 {
		appConfig.EmbeddingConfig.TimeoutSeconds = options.EmbeddingTimeoutSeconds
	}
}

// buildSearchEvalEmbeddingProvider 根据运行选项构造真实外部嵌入服务，避免评测误用本地伪向量。
func buildSearchEvalEmbeddingProvider(cfg *config.EmbeddingConfig, options SearchEvalRunOptions) (EmbeddingProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("search_eval 缺少 embedding 配置")
	}
	if !options.UseExternalEmbedding() {
		if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return nil, fmt.Errorf("search_eval 需要显式指定外部向量服务，或在 embedding 配置中提供可用的 baseUrl/model")
		}
		provider := searchEvalExternalEmbeddingProvider{delegate: NewEmbeddingProvider(cfg)}
		if !provider.Enabled() {
			return nil, fmt.Errorf("search_eval 需要显式指定外部向量服务，或在 embedding 配置中提供可用的 baseUrl/model")
		}
		return provider, nil
	}
	provider := searchEvalExternalEmbeddingProvider{delegate: NewEmbeddingProvider(cfg)}
	if !provider.Enabled() {
		return nil, fmt.Errorf("外部向量服务配置不完整，无法启用 embedding provider")
	}
	return provider, nil
}

func mergeSearchEvalEmbeddingConfig(base, override *SearchEvalEmbeddingConfig) *SearchEvalEmbeddingConfig {
	if base == nil && override == nil {
		return nil
	}
	merged := &SearchEvalEmbeddingConfig{}
	if base != nil {
		*merged = *base
	}
	if override == nil {
		return merged
	}
	if override.SemanticSimilarityThreshold != nil {
		merged.SemanticSimilarityThreshold = override.SemanticSimilarityThreshold
	}
	if override.BaseURL != nil {
		merged.BaseURL = override.BaseURL
	}
	if override.APIKey != nil {
		merged.APIKey = override.APIKey
	}
	if override.Model != nil {
		merged.Model = override.Model
	}
	if override.TimeoutSeconds != nil {
		merged.TimeoutSeconds = override.TimeoutSeconds
	}
	if override.SemanticHitFetchLimit != nil {
		merged.SemanticHitFetchLimit = override.SemanticHitFetchLimit
	}
	merged.SemanticWindow = mergeSearchEvalSemanticWindow(baseValueEmbeddingWindow(base), override.SemanticWindow)
	merged.Decay = mergeSearchEvalDecay(baseValueEmbeddingDecay(base), override.Decay)
	return merged
}

func mergeSearchEvalSearchConfig(base, override *SearchEvalSearchConfig) *SearchEvalSearchConfig {
	if base == nil && override == nil {
		return nil
	}
	merged := &SearchEvalSearchConfig{}
	if base != nil {
		*merged = *base
	}
	if override == nil {
		return merged
	}
	merged.Keyword = mergeSearchEvalKeyword(baseValueSearchKeyword(base), override.Keyword)
	merged.Fusion = mergeSearchEvalFusion(baseValueSearchFusion(base), override.Fusion)
	return merged
}

func mergeSearchEvalSemanticWindow(base, override *SearchEvalSemanticWindowCfg) *SearchEvalSemanticWindowCfg {
	if base == nil && override == nil {
		return nil
	}
	merged := &SearchEvalSemanticWindowCfg{}
	if base != nil {
		*merged = *base
	}
	if override == nil {
		return merged
	}
	if override.Mode != nil {
		merged.Mode = override.Mode
	}
	if override.BaseMaxCount != nil {
		merged.BaseMaxCount = override.BaseMaxCount
	}
	if override.DynamicMinCount != nil {
		merged.DynamicMinCount = override.DynamicMinCount
	}
	if override.DynamicMaxCount != nil {
		merged.DynamicMaxCount = override.DynamicMaxCount
	}
	if override.DynamicRatio != nil {
		merged.DynamicRatio = override.DynamicRatio
	}
	return merged
}

func mergeSearchEvalDecay(base, override *SearchEvalDecayCfg) *SearchEvalDecayCfg {
	if base == nil && override == nil {
		return nil
	}
	merged := &SearchEvalDecayCfg{}
	if base != nil {
		*merged = *base
	}
	if override == nil {
		return merged
	}
	if override.Enabled != nil {
		merged.Enabled = override.Enabled
	}
	if override.AgeWeight != nil {
		merged.AgeWeight = override.AgeWeight
	}
	if override.SemanticWeight != nil {
		merged.SemanticWeight = override.SemanticWeight
	}
	merged.HalfLifeDays = mergeSearchEvalHalfLife(baseValueDecayHalfLife(base), override.HalfLifeDays)
	return merged
}

func mergeSearchEvalHalfLife(base, override *SearchEvalHalfLifeDaysCfg) *SearchEvalHalfLifeDaysCfg {
	if base == nil && override == nil {
		return nil
	}
	merged := &SearchEvalHalfLifeDaysCfg{}
	if base != nil {
		*merged = *base
	}
	if override == nil {
		return merged
	}
	if override.Summary != nil {
		merged.Summary = override.Summary
	}
	if override.Error != nil {
		merged.Error = override.Error
	}
	return merged
}

func mergeSearchEvalKeyword(base, override *SearchEvalKeywordCfg) *SearchEvalKeywordCfg {
	if base == nil && override == nil {
		return nil
	}
	merged := &SearchEvalKeywordCfg{}
	if base != nil {
		*merged = *base
	}
	if override == nil {
		return merged
	}
	if override.Mode != nil {
		merged.Mode = override.Mode
	}
	return merged
}

func mergeSearchEvalFusion(base, override *SearchEvalFusionCfg) *SearchEvalFusionCfg {
	if base == nil && override == nil {
		return nil
	}
	merged := &SearchEvalFusionCfg{}
	if base != nil {
		*merged = *base
	}
	if override == nil {
		return merged
	}
	if override.Enabled != nil {
		merged.Enabled = override.Enabled
	}
	if override.Formula != nil {
		merged.Formula = override.Formula
	}
	if override.KeywordWeight != nil {
		merged.KeywordWeight = override.KeywordWeight
	}
	if override.SemanticWeight != nil {
		merged.SemanticWeight = override.SemanticWeight
	}
	if override.RecencyWeight != nil {
		merged.RecencyWeight = override.RecencyWeight
	}
	if override.MinSemanticScore != nil {
		merged.MinSemanticScore = override.MinSemanticScore
	}
	if override.CoverageDiscountBase != nil {
		merged.CoverageDiscountBase = override.CoverageDiscountBase
	}
	return merged
}

func baseValueEmbeddingWindow(cfg *SearchEvalEmbeddingConfig) *SearchEvalSemanticWindowCfg {
	if cfg == nil {
		return nil
	}
	return cfg.SemanticWindow
}
func baseValueEmbeddingDecay(cfg *SearchEvalEmbeddingConfig) *SearchEvalDecayCfg {
	if cfg == nil {
		return nil
	}
	return cfg.Decay
}
func baseValueDecayHalfLife(cfg *SearchEvalDecayCfg) *SearchEvalHalfLifeDaysCfg {
	if cfg == nil {
		return nil
	}
	return cfg.HalfLifeDays
}
func baseValueSearchKeyword(cfg *SearchEvalSearchConfig) *SearchEvalKeywordCfg {
	if cfg == nil {
		return nil
	}
	return cfg.Keyword
}
func baseValueSearchFusion(cfg *SearchEvalSearchConfig) *SearchEvalFusionCfg {
	if cfg == nil {
		return nil
	}
	return cfg.Fusion
}

func loadSearchEvalCorpusFile(filePath string) ([]SearchEvalCorpusRecord, error) {
	var items []SearchEvalCorpusRecord
	if err := decodeSearchEvalJSONL(filePath, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("评测语料为空: %s", filePath)
	}
	return items, nil
}

func loadSearchEvalQueriesFile(filePath string) ([]SearchEvalQueryRecord, error) {
	var items []SearchEvalQueryRecord
	if err := decodeSearchEvalJSONL(filePath, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("评测查询为空: %s", filePath)
	}
	return items, nil
}

func loadSearchEvalQrelsFile(filePath string) (map[string]SearchEvalQrelsRecord, error) {
	var items []SearchEvalQrelsRecord
	if err := decodeSearchEvalJSONL(filePath, &items); err != nil {
		return nil, err
	}
	out := make(map[string]SearchEvalQrelsRecord, len(items))
	for _, item := range items {
		out[item.QueryID] = item
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("评测标注为空: %s", filePath)
	}
	return out, nil
}

func loadSearchEvalExperimentFile(filePath string) (SearchEvalExperimentFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return SearchEvalExperimentFile{}, fmt.Errorf("读取实验配置失败: %w", err)
	}
	var cfg SearchEvalExperimentFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return SearchEvalExperimentFile{}, fmt.Errorf("解析实验配置失败: %w", err)
	}
	return cfg, nil
}

func decodeSearchEvalJSONL[T any](filePath string, target *[]T) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item T
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return fmt.Errorf("解析 JSONL 失败: file=%s line=%d err=%w", filePath, lineNo, err)
		}
		*target = append(*target, item)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("扫描文件失败: %w", err)
	}
	return nil
}

func seedSearchEvalCorpus(store *models.Store, provider EmbeddingProvider, corpus []SearchEvalCorpusRecord) (map[string]int64, error) {
	idMap := make(map[string]int64, len(corpus))
	for _, item := range corpus {
		createdAt := time.Now().UTC().Format(time.RFC3339Nano)
		rows, err := store.CreateMemories([]models.Memory{{UserID: "search-eval", ProjectName: item.ProjectName, GitBranch: "feature/search-eval", Type: item.Type, Title: item.Title, Tags: models.EncodeTags(item.Tags), Summary: item.Summary, Content: item.Content, Timestamp: item.Timestamp, CreatedAt: createdAt}})
		if err != nil {
			return nil, fmt.Errorf("写入评测语料失败: id=%s err=%w", item.ID, err)
		}
		row := memoryRowFromModel(rows[0])
		vectors, err := provider.EmbedTexts([]string{BuildMemoryEmbeddingText(row)})
		if err != nil {
			return nil, fmt.Errorf("生成评测向量失败: id=%s err=%w", item.ID, err)
		}
		if err := store.UpsertMemoryEmbeddings([]models.MemoryEmbedding{{MemoryID: rows[0].ID, ProjectName: rows[0].ProjectName, Type: rows[0].Type, Vector: models.EncodeVector(vectors[0]), Timestamp: rows[0].Timestamp, UpdatedAt: createdAt}}); err != nil {
			return nil, fmt.Errorf("写入评测向量失败: id=%s err=%w", item.ID, err)
		}
		idMap[item.ID] = rows[0].ID
	}
	return idMap, nil
}

func runSearchEvalQueries(ctx context.Context, service *Service, queries []SearchEvalQueryRecord, qrels map[string]SearchEvalQrelsRecord, idMap map[string]int64) ([]SearchEvalResult, error) {
	results := make([]SearchEvalResult, 0, len(queries))
	for _, query := range queries {
		relevance, ok := qrels[query.QueryID]
		if !ok {
			return nil, fmt.Errorf("缺少查询标注: %s", query.QueryID)
		}
		startedAt := time.Now()
		result, err := service.Search(ctx, query.ProjectName, query.Queries, false)
		if err != nil {
			return nil, fmt.Errorf("执行评测查询失败: query_id=%s err=%w", query.QueryID, err)
		}
		ranking := collectSearchEvalRanking(result, query.SourceTypes)
		externalRanking, err := convertSearchEvalRankingToExternalIDs(ranking, idMap)
		if err != nil {
			return nil, err
		}
		results = append(results, SearchEvalResult{Query: query, Ranking: externalRanking, LatencyMS: float64(time.Since(startedAt).Microseconds()) / 1000, RecallAt5: recallAtK(externalRanking, relevance.RelevantIDs, 5), MRRAt10: mrrAtK(externalRanking, relevance.RelevantIDs, 10), NDCGAt10: ndcgAtK(externalRanking, relevance.GradedRelevance, 10), Top1Correct: top1Matches(externalRanking, relevance.ExpectedTop1)})
	}
	return results, nil
}

func collectSearchEvalRanking(result SearchResult, sourceTypes []string) []int64 {
	allowed := map[string]struct{}{}
	for _, sourceType := range sourceTypes {
		allowed[strings.ToLower(strings.TrimSpace(sourceType))] = struct{}{}
	}
	hits := make([]Hit, 0, len(result.ErrorHits)+len(result.SummaryHits))
	if _, ok := allowed["error"]; ok {
		hits = append(hits, result.ErrorHits...)
	}
	if _, ok := allowed["summary"]; ok {
		hits = append(hits, result.SummaryHits...)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Confidence == hits[j].Confidence {
			return hits[i].Timestamp.After(hits[j].Timestamp)
		}
		return hits[i].Confidence > hits[j].Confidence
	})
	ranking := make([]int64, 0, len(hits))
	seen := map[int64]struct{}{}
	for _, hit := range hits {
		if _, ok := seen[hit.ID]; ok {
			continue
		}
		seen[hit.ID] = struct{}{}
		ranking = append(ranking, hit.ID)
	}
	return ranking
}

func convertSearchEvalRankingToExternalIDs(ranking []int64, idMap map[string]int64) ([]string, error) {
	reverse := map[int64]string{}
	for externalID, databaseID := range idMap {
		reverse[databaseID] = externalID
	}
	out := make([]string, 0, len(ranking))
	for _, databaseID := range ranking {
		externalID, ok := reverse[databaseID]
		if !ok {
			return nil, fmt.Errorf("缺少数据库主键映射: %d", databaseID)
		}
		out = append(out, externalID)
	}
	return out, nil
}

func summarizeSearchEvalResults(results []SearchEvalResult) SearchEvalSummary {
	if len(results) == 0 {
		return SearchEvalSummary{}
	}
	latencies := make([]float64, 0, len(results))
	var recallAt5, mrrAt10, ndcgAt10, top1, errorRecall float64
	errorQueryCount := 0
	for _, result := range results {
		latencies = append(latencies, result.LatencyMS)
		recallAt5 += result.RecallAt5
		mrrAt10 += result.MRRAt10
		ndcgAt10 += result.NDCGAt10
		if result.Top1Correct {
			top1 += 1
		}
		if containsSearchEvalSourceType(result.Query.SourceTypes, "error") {
			errorRecall += result.RecallAt5
			errorQueryCount++
		}
	}
	return SearchEvalSummary{RecallAt5: recallAt5 / float64(len(results)), MRRAt10: mrrAt10 / float64(len(results)), NDCGAt10: ndcgAt10 / float64(len(results)), Top1Accuracy: top1 / float64(len(results)), ErrorRecallAt5: safeSearchEvalAverage(errorRecall, errorQueryCount), P95LatencyMS: percentileFloat(latencies, 0.95)}
}

// diffSearchEvalSummary 统一计算相对基线的指标变化，避免报告层重复手工求差值。
func diffSearchEvalSummary(current, baseline SearchEvalSummary) SearchEvalSummaryDelta {
	return SearchEvalSummaryDelta{
		RecallAt5:      current.RecallAt5 - baseline.RecallAt5,
		MRRAt10:        current.MRRAt10 - baseline.MRRAt10,
		NDCGAt10:       current.NDCGAt10 - baseline.NDCGAt10,
		Top1Accuracy:   current.Top1Accuracy - baseline.Top1Accuracy,
		ErrorRecallAt5: current.ErrorRecallAt5 - baseline.ErrorRecallAt5,
		P95LatencyMS:   current.P95LatencyMS - baseline.P95LatencyMS,
	}
}

func buildSearchEvalFailures(results []SearchEvalResult, qrels map[string]SearchEvalQrelsRecord) []SearchEvalFailure {
	failures := []SearchEvalFailure{}
	for _, result := range results {
		relevance := qrels[result.Query.QueryID]
		if result.Top1Correct && result.RecallAt5 >= 1 {
			continue
		}
		limit := searchEvalMinInt(5, len(result.Ranking))
		failures = append(failures, SearchEvalFailure{QueryID: result.Query.QueryID, Category: result.Query.Category, ExpectedTop1: relevance.ExpectedTop1, ActualTop5: append([]string(nil), result.Ranking[:limit]...), RecallAt5: result.RecallAt5, MRRAt10: result.MRRAt10, Top1Correct: result.Top1Correct})
	}
	return failures
}

func validateSearchEvalReport(summary, baseline SearchEvalSummary, acceptance SearchEvalAcceptance) (bool, []string) {
	errors := []string{}
	if summary.RecallAt5 < acceptance.RecallAt5 {
		errors = append(errors, fmt.Sprintf("Recall@5=%.4f < %.4f", summary.RecallAt5, acceptance.RecallAt5))
	}
	if summary.MRRAt10 < acceptance.MRRAt10 {
		errors = append(errors, fmt.Sprintf("MRR@10=%.4f < %.4f", summary.MRRAt10, acceptance.MRRAt10))
	}
	if summary.ErrorRecallAt5 < acceptance.ErrorRecallAt5 {
		errors = append(errors, fmt.Sprintf("ErrorRecall@5=%.4f < %.4f", summary.ErrorRecallAt5, acceptance.ErrorRecallAt5))
	}
	if baseline.P95LatencyMS > 0 && summary.P95LatencyMS > baseline.P95LatencyMS*acceptance.MaxP95LatencyRatioVsBase {
		errors = append(errors, fmt.Sprintf("P95LatencyMS=%.3f > baseline %.3f * %.2f", summary.P95LatencyMS, baseline.P95LatencyMS, acceptance.MaxP95LatencyRatioVsBase))
	}
	return len(errors) == 0, errors
}

func renderSearchEvalMarkdown(report SearchEvalSuiteReport) string {
	lines := []string{"# Search Eval Report", "", fmt.Sprintf("- 生成时间: %s", report.GeneratedAt), fmt.Sprintf("- 向量提供方式: %s", report.Embedding.Mode), fmt.Sprintf("- 向量服务地址: %s", searchEvalFallbackText(report.Embedding.BaseURL, "(local)")), fmt.Sprintf("- 向量模型: %s", searchEvalFallbackText(report.Embedding.Model, "(local)")), fmt.Sprintf("- 超时时间: %.0fs", report.Embedding.TimeoutSeconds), fmt.Sprintf("- 基线 Recall@5 门槛: %.2f", report.Acceptance.RecallAt5), fmt.Sprintf("- 基线 MRR@10 门槛: %.2f", report.Acceptance.MRRAt10), fmt.Sprintf("- 错误类 Recall@5 门槛: %.2f", report.Acceptance.ErrorRecallAt5), "", "## Recommendation", renderSearchEvalRecommendationBlock(report.Recommendation), "", "## Baseline", renderSearchEvalReportBlock(report.Baseline)}
	if len(report.Experiments) > 0 {
		lines = append(lines, "", "## Experiments")
		for _, experiment := range report.Experiments {
			lines = append(lines, renderSearchEvalReportBlock(experiment))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func renderSearchEvalRecommendationBlock(recommendation SearchEvalRecommendation) string {
	lines := []string{fmt.Sprintf("- 推荐方案: %s", recommendation.ExperimentName), fmt.Sprintf("- 综合评分: %.4f", recommendation.Score), fmt.Sprintf("- Recall@5: %.4f (%+.4f)", recommendation.Summary.RecallAt5, recommendation.Delta.RecallAt5), fmt.Sprintf("- MRR@10: %.4f (%+.4f)", recommendation.Summary.MRRAt10, recommendation.Delta.MRRAt10), fmt.Sprintf("- ErrorRecall@5: %.4f (%+.4f)", recommendation.Summary.ErrorRecallAt5, recommendation.Delta.ErrorRecallAt5), fmt.Sprintf("- P95LatencyMS: %.3f (%+.3f)", recommendation.Summary.P95LatencyMS, recommendation.Delta.P95LatencyMS)}
	if len(recommendation.AppliedExperiments) > 0 {
		lines = append(lines, fmt.Sprintf("- 自动调优路径: %s", strings.Join(recommendation.AppliedExperiments, " -> ")))
	}
	if len(recommendation.Reasons) > 0 {
		lines = append(lines, "- 调整理由:")
		for _, reason := range recommendation.Reasons {
			lines = append(lines, fmt.Sprintf("  - %s", reason))
		}
	}
	if len(recommendation.ConfigPatch) > 0 {
		if patchJSON, err := json.MarshalIndent(recommendation.ConfigPatch, "", "  "); err == nil {
			lines = append(lines, "- 推荐配置片段:", "```json", string(patchJSON), "```")
		}
	}
	return strings.Join(lines, "\n")
}

// searchEvalFallbackText 统一渲染可选文本字段，避免报告里出现空字符串难以辨认。
func searchEvalFallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func renderSearchEvalReportBlock(report SearchEvalExperimentReport) string {
	status := "PASS"
	if !report.Passed {
		status = "FAIL"
	}
	lines := []string{fmt.Sprintf("### %s", report.Name), fmt.Sprintf("- 状态: %s", status), fmt.Sprintf("- Recall@5: %.4f (%+.4f)", report.Summary.RecallAt5, report.Delta.RecallAt5), fmt.Sprintf("- MRR@10: %.4f (%+.4f)", report.Summary.MRRAt10, report.Delta.MRRAt10), fmt.Sprintf("- nDCG@10: %.4f (%+.4f)", report.Summary.NDCGAt10, report.Delta.NDCGAt10), fmt.Sprintf("- Top1Accuracy: %.4f (%+.4f)", report.Summary.Top1Accuracy, report.Delta.Top1Accuracy), fmt.Sprintf("- ErrorRecall@5: %.4f (%+.4f)", report.Summary.ErrorRecallAt5, report.Delta.ErrorRecallAt5), fmt.Sprintf("- P95LatencyMS: %.3f (%+.3f)", report.Summary.P95LatencyMS, report.Delta.P95LatencyMS)}
	if len(report.ValidationError) > 0 {
		lines = append(lines, "- 验证失败: "+strings.Join(report.ValidationError, "; "))
	}
	if len(report.Failures) > 0 {
		lines = append(lines, "- 失败样本:")
		for _, failure := range report.Failures[:searchEvalMinInt(5, len(report.Failures))] {
			lines = append(lines, fmt.Sprintf("  - %s %s top5=%v", failure.QueryID, failure.Category, failure.ActualTop5))
		}
	}
	return strings.Join(lines, "\n")
}

func containsSearchEvalSourceType(sourceTypes []string, target string) bool {
	for _, sourceType := range sourceTypes {
		if strings.EqualFold(strings.TrimSpace(sourceType), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func safeSearchEvalAverage(sum float64, count int) float64 {
	if count <= 0 {
		return 0
	}
	return sum / float64(count)
}

func percentileFloat(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if percentile <= 0 {
		percentile = 0
	}
	if percentile > 1 {
		percentile = 1
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(math.Ceil(float64(len(sorted))*percentile)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func recallAtK(ranking []string, relevantIDs []string, k int) float64 {
	if len(relevantIDs) == 0 || k <= 0 {
		return 0
	}
	relevant := map[string]struct{}{}
	for _, id := range relevantIDs {
		relevant[id] = struct{}{}
	}
	hitCount := 0
	for idx, id := range ranking {
		if idx >= k {
			break
		}
		if _, ok := relevant[id]; ok {
			hitCount++
		}
	}
	return float64(hitCount) / float64(len(relevantIDs))
}

func mrrAtK(ranking []string, relevantIDs []string, k int) float64 {
	if len(relevantIDs) == 0 || k <= 0 {
		return 0
	}
	relevant := map[string]struct{}{}
	for _, id := range relevantIDs {
		relevant[id] = struct{}{}
	}
	for idx, id := range ranking {
		if idx >= k {
			break
		}
		if _, ok := relevant[id]; ok {
			return 1 / float64(idx+1)
		}
	}
	return 0
}

func ndcgAtK(ranking []string, gradedRelevance map[string]int, k int) float64 {
	if len(gradedRelevance) == 0 || k <= 0 {
		return 0
	}
	actual := 0.0
	for idx, id := range ranking {
		if idx >= k {
			break
		}
		actual += dcgGain(gradedRelevance[id], idx)
	}
	idealGrades := make([]int, 0, len(gradedRelevance))
	for _, gain := range gradedRelevance {
		idealGrades = append(idealGrades, gain)
	}
	sort.Slice(idealGrades, func(i, j int) bool { return idealGrades[i] > idealGrades[j] })
	ideal := 0.0
	for idx, gain := range idealGrades {
		if idx >= k {
			break
		}
		ideal += dcgGain(gain, idx)
	}
	if ideal <= 0 {
		return 0
	}
	return actual / ideal
}

func dcgGain(relevance int, index int) float64 {
	if relevance <= 0 {
		return 0
	}
	return (math.Pow(2, float64(relevance)) - 1) / math.Log2(float64(index+2))
}

func top1Matches(ranking []string, expectedTop1 []string) bool {
	if len(ranking) == 0 || len(expectedTop1) == 0 {
		return false
	}
	for _, expected := range expectedTop1 {
		if ranking[0] == expected {
			return true
		}
	}
	return false
}

func searchEvalMinInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func cloneSearchEvalAppConfig(configValue config.AppConfig) config.AppConfig {
	cloned := configValue
	if configValue.DatabaseConfig != nil {
		databaseConfig := *configValue.DatabaseConfig
		cloned.DatabaseConfig = &databaseConfig
	}
	if configValue.EmbeddingConfig != nil {
		embeddingConfig := *configValue.EmbeddingConfig
		cloned.EmbeddingConfig = &embeddingConfig
	}
	if configValue.SearchConfig != nil {
		searchConfig := *configValue.SearchConfig
		if configValue.SearchConfig.KeywordFields != nil {
			searchConfig.KeywordFields = append([]string(nil), configValue.SearchConfig.KeywordFields...)
		}
		if configValue.SearchConfig.KeywordFieldWeights != nil {
			searchConfig.KeywordFieldWeights = make(map[string]float64, len(configValue.SearchConfig.KeywordFieldWeights))
			for key, value := range configValue.SearchConfig.KeywordFieldWeights {
				searchConfig.KeywordFieldWeights[key] = value
			}
		}
		if configValue.SearchConfig.KeywordSynonymGroups != nil {
			searchConfig.KeywordSynonymGroups = make([][]string, 0, len(configValue.SearchConfig.KeywordSynonymGroups))
			for _, group := range configValue.SearchConfig.KeywordSynonymGroups {
				searchConfig.KeywordSynonymGroups = append(searchConfig.KeywordSynonymGroups, append([]string(nil), group...))
			}
		}
		cloned.SearchConfig = &searchConfig
	}
	if configValue.ScheduleConfig != nil {
		scheduleConfig := *configValue.ScheduleConfig
		cloned.ScheduleConfig = &scheduleConfig
	}
	return cloned
}

func cloneSearchEvalAppConfigPtr(configValue config.AppConfig) *config.AppConfig {
	cloned := cloneSearchEvalAppConfig(configValue)
	return &cloned
}

func boolPtr(value bool) *bool          { return &value }
func intPtr(value int) *int             { return &value }
func float64Ptr(value float64) *float64 { return &value }
func stringPtr(value string) *string    { return &value }
