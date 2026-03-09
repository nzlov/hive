package main

import (
	"flag"
	"fmt"

	"memory-manager/internal/api"
)

// runRebuildEmbeddingsCommand 通过 HTTP 调用服务端重建向量，避免脚本继续持有维护逻辑。
func runRebuildEmbeddingsCommand(client *client, args []string) (int, error) {
	fs := flag.NewFlagSet("rebuild-embeddings", flag.ContinueOnError)
	root := fs.String("root", ".", "项目根目录")
	force := fs.Bool("force", false, "强制重建")
	if err := fs.Parse(args); err != nil {
		return 1, err
	}
	projectRoot, err := resolveProjectRoot(*root)
	if err != nil {
		return 1, err
	}
	response, err := client.rebuildEmbeddings(api.RebuildRequest{ProjectRoot: projectRoot, Force: *force})
	if err != nil {
		return 1, err
	}
	fmt.Println(response.Message)
	return 0, nil
}
