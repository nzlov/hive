#!/usr/bin/env bash
set -euo pipefail

# 这个脚本基于 ncruces/sqlite-vec-go 思路构建项目内 sqlite3.wasm。
# 可通过环境变量覆盖版本或完整下载地址，避免每次改脚本才能升级。
# 示例:
#   SQLITE_VEC_VERSION=0.1.7-alpha.10 ./internal/sqlitevecembed/build.sh
#   GO_SQLITE3_TAG=v0.30.5 ./internal/sqlitevecembed/build.sh
#   SQLITE_VEC=https://example/sqlite-vec-amalgamation.tar.gz ./internal/sqlitevecembed/build.sh

ROOT_DIR="$(cd -P -- "$(dirname -- "$0")" && pwd)"
WORK_DIR="$ROOT_DIR/.build"

GO_SQLITE3_TAG="${GO_SQLITE3_TAG:-v0.30.5}"
SQLITE_VEC_VERSION="${SQLITE_VEC_VERSION:-0.1.6}"

GO_SQLITE3="${GO_SQLITE3:-https://github.com/ncruces/go-sqlite3/archive/refs/tags/${GO_SQLITE3_TAG}.tar.gz}"
SQLITE_VEC="${SQLITE_VEC:-https://github.com/asg017/sqlite-vec/releases/download/v${SQLITE_VEC_VERSION}/sqlite-vec-${SQLITE_VEC_VERSION}-amalgamation.tar.gz}"

rm -rf "$WORK_DIR"
mkdir -p "$WORK_DIR/output"

curl -#L "$GO_SQLITE3" | tar xzC "$WORK_DIR/output" --strip-components=1

"$WORK_DIR/output/sqlite3/tools.sh"
"$WORK_DIR/output/sqlite3/download.sh"

curl -#L "$SQLITE_VEC" | tar xzC "$WORK_DIR/output/sqlite3"

# 为什么要补这个宏：WASM 环境无传统文件系统，省掉文件接口可减少初始化不确定性。
perl -0pi -e 'if (index($_, "-DSQLITE_VEC_OMIT_FS=1") < 0) { s/-DSQLITE_CUSTOM_INCLUDE=sqlite_opt\.h \\\\n\t\$\(awk/-DSQLITE_CUSTOM_INCLUDE=sqlite_opt.h \\\\n\t-DSQLITE_VEC_OMIT_FS=1 \\\\n\t\$\(awk/s }' "$WORK_DIR/output/embed/build.sh"

# 为什么要改 main.c：把 sqlite-vec 编进 wasm，并在初始化时自动注册扩展。
perl -0pi -e 'if (index($_, "#include \"sqlite-vec.c\"") < 0) { s/#include "vtab\.c"\n/#include "vtab.c"\n#include "sqlite-vec.c"\n/s }' "$WORK_DIR/output/sqlite3/main.c"
perl -0pi -e 'if (index($_, "sqlite3_vec_init") < 0) { s/sqlite3_auto_extension\(\(void \(\*\)\(void\)\)sqlite3_time_init\);\n/sqlite3_auto_extension((void (*)(void))sqlite3_time_init);\n  sqlite3_auto_extension((void (*)(void))sqlite3_vec_init);\n/s }' "$WORK_DIR/output/sqlite3/main.c"

"$WORK_DIR/output/embed/build.sh"

mv "$WORK_DIR/output/embed/sqlite3.wasm" "$ROOT_DIR/sqlite3.wasm"
rm -rf "$WORK_DIR"
