package main

import (
	"flag"
	"fmt"
	"time"
)

const embeddingModelMetaKey = "embedding_model"

// rebuildResult 统一返回是否执行和结果文案，方便命令行与内部调用共用。
type rebuildResult struct {
	Changed bool
	Message string
}

// runRebuildCommand 提供独立子命令入口，便于手动维护与自动触发复用同一逻辑。
func runRebuildCommand(args []string) error {
	fs := flag.NewFlagSet("rebuild-memory-embeddings", flag.ContinueOnError)
	root := fs.String("root", ".", "Project root that contains .memory/")
	force := fs.Bool("force", false, "Rebuild even when configured model matches database metadata")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := rebuildEmbeddings(*root, *force)
	if err != nil {
		return err
	}
	fmt.Println(result.Message)
	return nil
}

// rebuildEmbeddings 在模型变化时全量重建向量，避免新旧维度混用。
func rebuildEmbeddings(projectRoot string, force bool) (rebuildResult, error) {
	location, err := resolveMemoryLocation(projectRoot)
	if err != nil {
		return rebuildResult{}, err
	}
	provider := createEmbeddingProvider(location.EmbeddingConfig)
	if !provider.Enabled() {
		return rebuildResult{Changed: false, Message: "未配置嵌入模型，跳过重建。"}, nil
	}
	db, err := connectMemoryDB(location.MemoryRoot)
	if err != nil {
		return rebuildResult{}, err
	}
	defer db.Close()

	currentModel, err := getMemoryMetadata(db, embeddingModelMetaKey)
	if err != nil {
		return rebuildResult{}, err
	}
	rows, err := fetchAllMemories(db)
	if err != nil {
		return rebuildResult{}, err
	}
	var embeddingCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_embeddings`).Scan(&embeddingCount); err != nil {
		return rebuildResult{}, err
	}
	if currentModel == provider.ModelName() && embeddingCount == len(rows) && !force {
		return rebuildResult{Changed: false, Message: fmt.Sprintf("嵌入模型未变化，继续使用 %s。", provider.ModelName())}, nil
	}

	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, buildMemoryEmbeddingText(row))
	}
	vectors, err := provider.EmbedTexts(texts)
	if err != nil {
		return rebuildResult{}, err
	}
	if err := deleteAllMemoryEmbeddings(db); err != nil {
		return rebuildResult{}, err
	}
	rebuiltAt := time.Now().UTC().Format(time.RFC3339Nano)
	for idx, row := range rows {
		if idx >= len(vectors) {
			break
		}
		if err := upsertMemoryEmbedding(db, row.ID, vectors[idx], rebuiltAt); err != nil {
			return rebuildResult{}, err
		}
	}
	if err := setMemoryMetadata(db, embeddingModelMetaKey, provider.ModelName(), rebuiltAt); err != nil {
		return rebuildResult{}, err
	}
	return rebuildResult{Changed: true, Message: fmt.Sprintf("已使用模型 %s 重建 %d 条向量。", provider.ModelName(), len(vectors))}, nil
}
