#!/usr/bin/env bash
# 一条命令启动全部服务：模拟网络/分机接口 + Go/Gin API + Vue 前端（同一进程）
# 用法:
#   ./run.sh           启动（首次自动构建前端）
#   ./run.sh --reset   删除数据库重新初始化种子数据后启动
set -euo pipefail
cd "$(dirname "$0")"

export PATH="$HOME/.local/go/bin:$PATH"
ADDR="${ADDR:-:8080}"
MOCK_ADDR="${MOCK_ADDR:-127.0.0.1:9090}"
DB_PATH="${DB_PATH:-seatmove.db}"

if [[ "${1:-}" == "--reset" ]]; then
  echo ">> 重置数据库 $DB_PATH"
  rm -f "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm"
fi

# 1) 前端依赖与构建（产物通过 go:embed 打进二进制；SKIP_WEB_BUILD=1 可跳过）
if [[ ! -d web/node_modules ]]; then
  echo ">> 安装前端依赖…"
  (cd web && npm install)
fi
if [[ "${SKIP_WEB_BUILD:-}" != "1" ]]; then
  echo ">> 构建 Vue 前端…"
  (cd web && npm run build)
fi

# 2) 启动后端（内部同时拉起模拟接口 :9090）
echo ">> 启动服务: http://localhost${ADDR}  （模拟接口 http://${MOCK_ADDR}）"
exec go run .
