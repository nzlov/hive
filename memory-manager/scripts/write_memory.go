package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var invalidTitleCharsRegexp = regexp.MustCompile(`[^0-9A-Za-z_\-\p{Han}]`)

// MemoryWriteItem 统一描述单条待写入记忆，便于单条和批量入口共用一套校验。
type MemoryWriteItem struct {
	Type    string
	Title   string
	Tags    []string
	Summary string
	Context string
}

// runWriteCommand 根据配置将记忆写入项目内或外挂目录下的 SQLite 文件。
func runWriteCommand(args []string) error {
	fs := flag.NewFlagSet("write-memory", flag.ContinueOnError)
	root := fs.String("root", ".", "Project root that contains .memory/")
	itemsJSON := fs.String("items-json", "", "JSON object or array for batch writes")
	memType := fs.String("type", "", "Memory type")
	title := fs.String("title", "", "Memory title for filename suffix")
	tags := fs.String("tags", "", "Comma-separated tags")
	summary := fs.String("summary", "", "Short synopsis for YAML header")
	context := fs.String("context", "", "Natural language memory content")
	errorCode := fs.String("error-code", "", "Deprecated")
	fixCode := fs.String("fix-code", "", "Deprecated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = errorCode
	_ = fixCode

	location, err := resolveMemoryLocation(*root)
	if err != nil {
		return err
	}
	if _, err := rebuildEmbeddings(location.ProjectRoot, false); err != nil {
		return err
	}
	items, err := parseBatchItems(*itemsJSON, *memType, *title, *tags, *summary, *context)
	if err != nil {
		return err
	}
	provider := createEmbeddingProvider(location.EmbeddingConfig)
	db, err := connectMemoryDB(location.MemoryRoot)
	if err != nil {
		return err
	}
	defer db.Close()

	now := time.Now().UTC()
	timestampSeed := now.Unix()
	pendingRows := make([]MemoryRow, 0, len(items))
	pendingIDs := make([]int64, 0, len(items))

	for idx, item := range items {
		itemTime := time.Unix(timestampSeed+int64(idx), 0).UTC()
		timestamp := itemTime.Format("20060102150405")
		sanitizedTitle := sanitizeTitle(item.Title)
		content := buildMemoryContent(item.Type, location.ProjectName, sanitizedTitle, item.Tags, item.Summary, item.Context)
		result, err := db.Exec(
			`INSERT INTO memories (project_name, type, title, tags, summary, content, timestamp, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			location.ProjectName,
			item.Type,
			sanitizedTitle,
			encodeTags(item.Tags),
			item.Summary,
			content,
			timestamp,
			itemTime.Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}
		memoryID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		pendingIDs = append(pendingIDs, memoryID)
		pendingRows = append(pendingRows, MemoryRow{
			ID:          memoryID,
			ProjectName: location.ProjectName,
			Type:        item.Type,
			Title:       sanitizedTitle,
			Tags:        encodeTags(item.Tags),
			Summary:     item.Summary,
			Content:     content,
		})
	}

	if provider.Enabled() && len(pendingRows) > 0 {
		texts := make([]string, 0, len(pendingRows))
		for _, row := range pendingRows {
			texts = append(texts, buildMemoryEmbeddingText(row))
		}
		vectors, err := provider.EmbedTexts(texts)
		if err != nil {
			return err
		}
		updatedAt := now.Format(time.RFC3339Nano)
		for idx, memoryID := range pendingIDs {
			if idx >= len(vectors) {
				break
			}
			if err := upsertMemoryEmbedding(db, memoryID, vectors[idx], updatedAt); err != nil {
				return err
			}
		}
	}

	fmt.Println(getMemoryDBPath(location.MemoryRoot))
	return nil
}

// parseBatchItems 允许一次请求写入多条记忆，便于按目标或问题拆分总结。
func parseBatchItems(itemsJSON, memType, title, tags, summary, context string) ([]MemoryWriteItem, error) {
	if strings.TrimSpace(itemsJSON) == "" {
		if strings.TrimSpace(memType) == "" || strings.TrimSpace(title) == "" || strings.TrimSpace(context) == "" {
			return nil, fmt.Errorf("单条写入时必须提供 --type、--title 和 --context")
		}
		return []MemoryWriteItem{{
			Type:    strings.TrimSpace(memType),
			Title:   strings.TrimSpace(title),
			Tags:    splitTags(tags),
			Summary: strings.TrimSpace(summary),
			Context: context,
		}}, nil
	}

	var payload any
	if err := json.Unmarshal([]byte(itemsJSON), &payload); err != nil {
		return nil, fmt.Errorf("--items-json 必须是合法 JSON")
	}
	var rawItems []any
	switch value := payload.(type) {
	case map[string]any:
		rawItems = []any{value}
	case []any:
		rawItems = value
	default:
		return nil, fmt.Errorf("--items-json 必须是对象或对象数组")
	}

	items := make([]MemoryWriteItem, 0, len(rawItems))
	for idx, item := range rawItems {
		payloadItem, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("--items-json 第 %d 项必须是对象", idx+1)
		}
		memType = strings.TrimSpace(asString(payloadItem["type"]))
		if memType != "summary" && memType != "error" {
			return nil, fmt.Errorf("--items-json 第 %d 项的 type 必须是 summary 或 error", idx+1)
		}
		title = strings.TrimSpace(asString(payloadItem["title"]))
		if title == "" {
			return nil, fmt.Errorf("--items-json 第 %d 项缺少 title", idx+1)
		}
		context = strings.TrimSpace(asString(payloadItem["context"]))
		if context == "" {
			return nil, fmt.Errorf("--items-json 第 %d 项缺少 context", idx+1)
		}
		items = append(items, MemoryWriteItem{
			Type:    memType,
			Title:   title,
			Tags:    normalizeTags(payloadItem["tags"]),
			Summary: strings.TrimSpace(asString(payloadItem["summary"])),
			Context: context,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("--items-json 不能为空数组")
	}
	return items, nil
}

// sanitizeTitle 收敛标题字符集，避免数据库展示标题过长或混入异常字符。
func sanitizeTitle(title string) string {
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), "-")
	title = invalidTitleCharsRegexp.ReplaceAllString(title, "")
	if len([]rune(title)) > 48 {
		title = string([]rune(title)[:48])
	}
	if title == "" {
		return "memory"
	}
	return title
}

// splitTags 统一清洗逗号分隔标签，避免空标签进入索引。
func splitTags(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// normalizeTags 兼容字符串和数组写法，减少批量写入额外转换成本。
func normalizeTags(raw any) []string {
	switch value := raw.(type) {
	case string:
		return splitTags(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(asString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// yamlHeader 保留原有 Markdown 头部格式，减少搜索结果结构变化。
func yamlHeader(memType, projectName, title string, tags []string, summary string) string {
	synopsis := strings.TrimSpace(summary)
	if synopsis == "" {
		synopsis = "自动生成记忆"
	}
	return strings.Join([]string{
		"---",
		"type: " + memType,
		"project: " + projectName,
		"title: " + title,
		"tags: " + strings.Join(tags, ", "),
		"summary: " + synopsis,
		"---",
		"",
	}, "\n")
}

// markdownBody 确保写入内容始终有正文，避免空记录影响后续检索体验。
func markdownBody(context string) string {
	body := strings.TrimSpace(context)
	if body == "" {
		return "## Details\n\n暂无内容。"
	}
	return body
}

// buildMemoryContent 统一生成持久化 Markdown 内容，降低总结和错误记忆的维护分叉。
func buildMemoryContent(memType, projectName, title string, tags []string, summary, context string) string {
	return yamlHeader(memType, projectName, title, tags, summary) + markdownBody(context) + "\n"
}
