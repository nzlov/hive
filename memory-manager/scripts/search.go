package main

import (
	"flag"
	"fmt"

	"memory-manager/internal/api"
)

// runSearchCommand 只负责解析参数与打印结果，把搜索实现完全交给服务端。
func runSearchCommand(client *client, args []string) (int, error) {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	root := fs.String("root", ".", "项目根目录")
	queriesArg := multiStringFlag{}
	fs.Var(&queriesArg, "query", "搜索关键词或正则")
	debug := fs.Bool("debug", false, "输出调试信息")
	if err := fs.Parse(normalizeLegacySearchArgs(args)); err != nil {
		return 1, err
	}
	projectRoot, err := resolveProjectRoot(*root)
	if err != nil {
		return 1, err
	}
	queries, err := parseScriptQueries(queriesArg)
	if err != nil {
		return 1, err
	}
	if len(queries) == 0 {
		return 1, fmt.Errorf("--query 至少需要一个非空关键词")
	}
	response, err := client.search(api.SearchRequest{ProjectRoot: projectRoot, Queries: queries, Debug: *debug})
	if err != nil {
		return 1, err
	}
	fmt.Print(response.Markdown)
	return 0, nil
}

// multiStringFlag 兼容多次传入 `--query`，避免调用方式被强制改写。
type multiStringFlag []string

// String 满足 flag.Value 接口要求，让调试输出保留可读性。
func (m *multiStringFlag) String() string {
	return fmt.Sprint([]string(*m))
}

// Set 允许多次追加参数，保持旧调用习惯不变。
func (m *multiStringFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

// normalizeLegacySearchArgs 兼容旧入口里的 `-debug` 写法，降低迁移成本。
func normalizeLegacySearchArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "-debug" {
			normalized = append(normalized, "--debug")
			continue
		}
		normalized = append(normalized, arg)
	}
	return normalized
}
