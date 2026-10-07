# 同步站点源码到服务器（Windows 本地执行，裸机部署用）
# 服务器上首次同步后，登录 https://你的域名/admin 点「手动重建发布」即可生成静态页
# 用法：先改好下面的 $SERVER / $REMOTE，然后 powershell -File deploy\deploy.ps1

$SERVER = "root@你的服务器IP"        # ← 改成你的
$REMOTE = "/var/www/easyblog"        # ← 与 /etc/easyblog.env 的 SITE_ROOT 一致

Write-Host "==> 同步源码（不含构建产物/工具/数据）" -ForegroundColor Green
# scp 不支持排除，用 tar 打包走管道（Git Bash 自带 tar/ssh）
bash -c "tar czf - --exclude=public --exclude=public_tmp --exclude=public_old --exclude=tools --exclude=serverdata --exclude=server/easyblog-server.exe --exclude=server/clblog-server.exe --exclude=.git --exclude=docker . | ssh $SERVER 'mkdir -p $REMOTE && tar xzf - -C $REMOTE'"

Write-Host "==> 完成。服务器操作：" -ForegroundColor Green
Write-Host "    1. 确认 /etc/easyblog.env 里 SITE_ROOT=$REMOTE"
Write-Host "    2. systemctl restart easyblog-server"
Write-Host "    3. 打开 /admin 登录 → 点右下「手动重建发布」"
