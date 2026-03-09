package sqlitevecembed

import (
	_ "embed"

	"github.com/ncruces/go-sqlite3"
)

// binary 持有项目内自建的 sqlite3.wasm，确保运行时不再依赖外部过期仓库。
//
//go:embed sqlite3.wasm
var binary []byte

func init() {
	// 直接覆盖 go-sqlite3 的 Binary，确保所有新连接都具备 sqlite-vec 能力。
	sqlite3.Binary = binary
}
