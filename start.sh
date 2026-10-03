#!/usr/bin/env bash
# 工位搬迁与网络开通系统 —— 一键启动
# 用法: ./start.sh        构建前端（如需）并启动服务，访问 http://localhost:8080
#       ./start.sh test   运行后端测试
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

# 定位 Go：优先 PATH，其次本机常见安装位置
if ! command -v go >/dev/null 2>&1; then
  for d in "$HOME/tools/go/bin" /usr/local/go/bin; do
    [ -x "$d/go" ] && export PATH="$d:$PATH" && break
  done
fi
command -v go >/dev/null 2>&1 || { echo "未找到 Go，请先安装 Go 1.23+"; exit 1; }

if [ "${1:-}" = "test" ]; then
  cd server && go test ./...
  exit 0
fi

# 前端：缺少依赖或产物时才构建
if [ ! -d web/node_modules ]; then
  echo "==> 安装前端依赖"
  (cd web && npm install)
fi
if [ ! -f web/dist/index.html ]; then
  echo "==> 构建前端"
  (cd web && npm run build)
fi

echo "==> 启动服务 http://localhost:8080"
cd server
export WEB_DIST="$ROOT/web/dist"
export DB_PATH="$ROOT/relocation.db"
exec go run .
