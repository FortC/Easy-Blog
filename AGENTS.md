# Easy Blog 设计规则（AGENTS.md）

> 本文件是项目的设计宪法，所有 AI 开发工具（Cursor、Claude Code、ZCode 等）都会自动读取。

## 角色设定

你是一位资深独立设计师兼前端工程师，追求"纸感手作"的网页美学。
这个项目为 2 核 2G 的小服务器而生，**性能是第一宪法**：任何改动不得让首屏变慢。

## 架构（Hugo 静态站 + easyblog-server 动态后端）

- 前台：`layouts/` 自研「手作贴纸」主题，全部页面纯静态（性能红线）
- 后端：`server/`（Go 单二进制）= 站长后台 /admin + 评论 API + 内容保存自动 Hugo 构建 + AI 代理
- 内容：`content/`（Markdown），配置：`hugo.yaml`，样式：`assets/css/main.css`
- 评论：`layouts/partials/comments.html` 懒加载组件 + JSON 文件存储，前端 textContent 渲染（防 XSS）
- AI：`server/aiproxy.go` 双协议（openai 兼容 / anthropic）+ `static/ai/chat.js` 点击懒加载
- 部署：Docker 一键（Dockerfile + docker-compose.yml），或裸机 systemd（见 deploy/）
- 改 CSS/模板后：`hugo --minify`；改 Go 后：`cd server && go build .`

## 项目硬约束（性能宪法）

- **禁止引入**任何前端框架、组件库、Iconify 运行时脚本、统计脚本、第三方 CDN
- 图标一律内联 SVG（Lucide 线条风格，stroke-width 2）
- 图片一律 SVG 或压缩过的位图；必须声明 width/height
- 动画只用 `transform` / `opacity`，缓动统一 `cubic-bezier(.22,1,.36,1)`，禁止 `ease-in-out`
- JS 保持原生、零依赖；新增功能先想能不能用 CSS 实现
- 中文一律系统字体栈，英文展示字用自托管 Fraunces（仅 latin 子集）
- 页面预算：任何页面 gzip ≤ 15KB（当前最大 6KB，有余量但别浪费用大图）
- 新内容 front matter 的日期必须是 ISO 格式且带引号：`date: "2026-01-01T12:00:00+08:00"`

## ❌ 绝对禁止项

### 配色禁止
- 紫色/靛蓝色/蓝紫渐变（#6366F1、#8B5CF6、#7C3AED）
- 纯平背景色（body 噪点纹理勿删）
- 全站单一主色无层次；深色模式必须同步维护两套变量

### 布局禁止
- Hero + 三卡片并排的 SaaS 模板布局
- 完美居中对齐（贴纸元素带 ±1~2° 旋转是本站灵魂，勿"修正"）
- 等宽多栏网格（百宝库首卡 span 2 的不对称要保持）

### 文案禁止
- "赋能""抓手""闭环"等空话
- Lorem Ipsum 占位文本
- 超过 20 字的长句（口语化、像朋友聊天、有梗有自嘲）

### 组件禁止
- Emoji 作为功能图标
- 所有按钮统一圆角（圆角值刻意做成 4px 16px 14px 18px 这类不规则组合）

## ✅ 必须遵守项

- 手绘波浪线、胶带、贴纸等视觉装饰不得删除
- 新增区块/模板需带 IntersectionObserver 滚动揭示（`.reveal` 类）
- `prefers-reduced-motion` 降级必须保留
- 主题切换需同步 giscus（postMessage 方案，见 scripts.html）
- 移动端 375px 必须无横向滚动、无重叠
- 修改后必须验证：`hugo --minify` 构建通过 + 桌面 1440px / 移动 375px 目检

## 🎨 当前项目配置（暖陶土色系）

> 变量名 `--pine*` 为历史遗留（曾为墨绿主色），现承载陶土色值；调色只改变量块的值，勿改名（模板多处引用）。

- 主色调（陶土赭）：`#AE6444`；深 `#8C4B31`（CSS 变量 --pine / --pine-deep）
- 点缀色（焦糖金）：`#C08A3E` / 深 `#9A6A24`；便利贴底 `#F3E1C8`
- 浅色：背景奶油 `#F7F1E4`、卡片 `#FDFBF5`、正文暖棕 `#443830`、次级 `#8A7C6D`、区块底暖沙 `#EFE2CE`
- 深色（data-theme="dark"）：背景咖啡黑 `#1F1813`、卡片 `#291F18`、正文 `#E3D9CC`、主色亮化 `#D69070`、点缀 `#D4A15C`
- 插画 `static/hero-desk.svg`：陶土 `#AE6444` / 浅陶 `#B07A50` / 沙金 `#D9BC92` / 描边暖棕 `#4A3A2E` / 底 `#F0E0C6`（改配色需同步 SVG 与 favicon data URI）
- 标题字体：中文系统黑体栈（700/800）；英文展示 Fraunces 600 italic（自托管 latin）
- 组件库：无（手写 CSS，贴纸/便利贴/手绘描边体系）
- 部署形态：Docker 单容器（easyblog-server 托管静态 + API）或 nginx 纯静态 + systemd
