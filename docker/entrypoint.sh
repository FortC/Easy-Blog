#!/bin/sh
# Easy Blog 容器入口：
#   1. 以 root 进来时修正 /data 属主，随后降权到 eblog 用户
#   2. 首次启动把示例站点种入 /data（此后你的内容/hugo.yaml 永不被镜像覆盖）
#   3. 每次启动同步主题文件（layouts/assets/static，改主题请构建自己的镜像）
#   4. 缺构建产物时先跑一次 Hugo，再拉起 easyblog-server
set -e

SITE_SRC=/app/site
THEME_SRC=/app/theme
DATA=/data

if [ "$(id -u)" = "0" ]; then
  chown -R eblog:eblog "$DATA"
  exec gosu eblog:eblog "$0" "$@"
fi

if [ ! -f "$DATA/hugo.yaml" ]; then
  echo "[entrypoint] 首次启动：初始化示例站点到 $DATA"
  cp -r "$SITE_SRC/." "$DATA/"
fi

echo "[entrypoint] 同步主题文件（layouts/assets/static）"
rm -rf "$DATA/layouts" "$DATA/assets" "$DATA/static"
cp -r "$THEME_SRC/layouts" "$THEME_SRC/assets" "$THEME_SRC/static" "$DATA/"

mkdir -p "$DATA/serverdata"

# 每次启动都构建：站点构建只要几十毫秒，保证主题/模板升级后不残留旧页面
echo "[entrypoint] 构建静态站点…"
hugo --minify --source "$DATA" -d "$DATA/public" \
  || echo "[entrypoint] 首次构建失败：登录 /admin 检查 hugo.yaml 后点「手动重建发布」"

export LISTEN="${LISTEN:-0.0.0.0:8080}"
export SITE_ROOT="$DATA"
export DATA_DIR="$DATA/serverdata"
export STATIC_SERVE="${STATIC_SERVE:-1}"
export HUGO_BIN="${HUGO_BIN:-/usr/local/bin/hugo}"

echo "[entrypoint] starting easyblog-server on $LISTEN"
exec /usr/local/bin/easyblog-server
