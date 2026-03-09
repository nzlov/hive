package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"memory-manager/internal/api"
)

// client 负责通过 HTTP 调用服务端，避免脚本继续直接操作数据库和嵌入服务。
type client struct {
	baseURL    string
	httpClient *http.Client
}

// newClient 基于本地配置创建 HTTP 客户端，让所有子命令共享同一服务地址来源。
func newClient() (*client, error) {
	cfg, err := loadScriptConfig()
	if err != nil {
		return nil, err
	}
	return &client{baseURL: strings.TrimRight(cfg.ServerBaseURL, "/"), httpClient: &http.Client{Timeout: 30 * time.Second}}, nil
}

// search 调用服务端搜索接口，把检索实现完全交给服务端维护。
func (c *client) search(request api.SearchRequest) (api.SearchResponse, error) {
	var response api.SearchResponse
	err := c.postJSON("/api/v1/memories/search", request, &response)
	return response, err
}

// write 调用服务端写入接口，让脚本只保留参数整理职责。
func (c *client) write(request api.WriteRequest) (api.WriteResponse, error) {
	var response api.WriteResponse
	err := c.postJSON("/api/v1/memories/write", request, &response)
	return response, err
}

// rebuildEmbeddings 调用服务端重建接口，避免脚本继续持有数据库维护逻辑。
func (c *client) rebuildEmbeddings(request api.RebuildRequest) (api.RebuildResponse, error) {
	var response api.RebuildResponse
	err := c.postJSON("/api/v1/memories/rebuild-embeddings", request, &response)
	return response, err
}

// postJSON 统一处理请求编码和错误解码，避免各子命令重复维护 HTTP 细节。
func (c *client) postJSON(path string, requestBody any, responseBody any) error {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err == nil {
			if message, ok := payload["error"].(string); ok && message != "" {
				return errors.New(message)
			}
		}
		return fmt.Errorf("服务端请求失败: %s", strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, responseBody); err != nil {
		return err
	}
	return nil
}
