#!/usr/bin/env bash
# 同步站点源码到服务器（macOS / Linux / Git Bash，裸机部署用）
# 用法：先改好下面的 SERVER / REMOTE，然后 bash deploy/deploy.sh
set -euo pipefail
SERVER="root@你的服务器IP"           # ← 改成你的
REMOTE="/var/www/easyblog"           # ← 与 /etc/easyblog.env 的 SITE_ROOT 一致

echo "==> 同步源码到 ${SERVER}:${REMOTE}"
tar czf - \
  --exclude=public --exclude=public_tmp --exclude=public_old \
  --exclude=tools --exclude=serverdata \
  --exclude=server/easyblog-server.exe --exclude=server/easyblog-server-linux-amd64 \
  --exclude=server/clblog-server.exe --exclude=server/clblog-server-linux-amd64 \
  --exclude=.git --exclude=docker \
  . | ssh "${SERVER}" "mkdir -p ${REMOTE} && tar xzf - -C ${REMOTE}"

echo "==> 完成。服务器侧：systemctl restart easyblog-server，然后 /admin 里点「手动重建发布」"
