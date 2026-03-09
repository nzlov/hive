package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"memory-manager/internal/api"
)

// runWriteCommand 只负责参数整理与结果输出，把实际写入和向量维护交给服务端。
func runWriteCommand(client *client, args []string) (int, error) {
	fs := flag.NewFlagSet("write", flag.ContinueOnError)
	root := fs.String("root", ".", "项目根目录")
	itemsJSON := fs.String("items-json", "", "批量写入 JSON")
	memType := fs.String("type", "", "记忆类型")
	title := fs.String("title", "", "记忆标题")
	tags := fs.String("tags", "", "逗号分隔标签")
	summary := fs.String("summary", "", "一句话摘要")
	context := fs.String("context", "", "Markdown 正文")
	errorCode := fs.String("error-code", "", "已废弃")
	fixCode := fs.String("fix-code", "", "已废弃")
	if err := fs.Parse(args); err != nil {
		return 1, err
	}
	_ = errorCode
	_ = fixCode
	projectRoot, err := resolveProjectRoot(*root)
	if err != nil {
		return 1, err
	}
	items, err := parseWriteItems(*itemsJSON, *memType, *title, *tags, *summary, *context)
	if err != nil {
		return 1, err
	}
	response, err := client.write(api.WriteRequest{ProjectRoot: projectRoot, Items: items})
	if err != nil {
		return 1, err
	}
	fmt.Println(response.DatabasePath)
	return 0, nil
}

// parseWriteItems 兼容单条和批量写入格式，避免脚本调用方因为协议变化被迫改造。
func parseWriteItems(itemsJSON, memType, title, tags, summary, context string) ([]api.MemoryWriteItem, error) {
	if strings.TrimSpace(itemsJSON) == "" {
		if strings.TrimSpace(memType) == "" || strings.TrimSpace(title) == "" || strings.TrimSpace(context) == "" {
			return nil, fmt.Errorf("单条写入时必须提供 --type、--title 和 --context")
		}
		return []api.MemoryWriteItem{{Type: strings.TrimSpace(memType), Title: strings.TrimSpace(title), Tags: splitTags(tags), Summary: strings.TrimSpace(summary), Context: context}}, nil
	}
	var payload any
	if err := json.Unmarshal([]byte(itemsJSON), &payload); err != nil {
		return nil, fmt.Errorf("--items-json 必须是合法 JSON")
	}
	var rawItems []any
	switch typed := payload.(type) {
	case map[string]any:
		rawItems = []any{typed}
	case []any:
		rawItems = typed
	default:
		return nil, fmt.Errorf("--items-json 必须是对象或对象数组")
	}
	items := make([]api.MemoryWriteItem, 0, len(rawItems))
	for idx, item := range rawItems {
		payloadItem, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("--items-json 第 %d 项必须是对象", idx+1)
		}
		parsedType := strings.TrimSpace(stringify(payloadItem["type"]))
		if parsedType != "summary" && parsedType != "error" {
			return nil, fmt.Errorf("--items-json 第 %d 项的 type 必须是 summary 或 error", idx+1)
		}
		parsedTitle := strings.TrimSpace(stringify(payloadItem["title"]))
		if parsedTitle == "" {
			return nil, fmt.Errorf("--items-json 第 %d 项缺少 title", idx+1)
		}
		parsedContext := strings.TrimSpace(stringify(payloadItem["context"]))
		if parsedContext == "" {
			return nil, fmt.Errorf("--items-json 第 %d 项缺少 context", idx+1)
		}
		items = append(items, api.MemoryWriteItem{Type: parsedType, Title: parsedTitle, Tags: normalizeTags(payloadItem["tags"]), Summary: strings.TrimSpace(stringify(payloadItem["summary"])), Context: parsedContext})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("--items-json 不能为空数组")
	}
	return items, nil
}
