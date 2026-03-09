package main

import (
	"fmt"
	"os"
)

// main 统一分发子命令，避免多个脚本入口长期重复维护参数解析逻辑。
func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

// run 根据子命令选择具体处理流程，让检索、写入和重建共享同一套基础能力。
func run(args []string) (int, error) {
	if len(args) == 0 {
		printUsage()
		return 1, nil
	}

	switch args[0] {
	case "search":
		return runSearchCommand(args[1:])
	case "write":
		return runWriteCommand(args[1:])
	case "rebuild-embeddings":
		return runRebuildEmbeddingsCommand(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return 0, nil
	default:
		printUsage()
		return 1, fmt.Errorf("不支持的子命令: %s", args[0])
	}
}

// printUsage 直接输出统一帮助，减少调用方在不同脚本之间切换时的认知成本。
func printUsage() {
	fmt.Println("用法: memory-manager <search|write|rebuild-embeddings> [参数]")
}
