# ---------- 阶段 1：编译 Go 后端（纯标准库，CGO 关闭产出静态二进制） ----------
FROM golang:1.23-alpine AS server
WORKDIR /src
COPY server/go.mod ./
COPY server/*.go ./
COPY server/admin ./admin
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/easyblog-server .

# ---------- 阶段 2：取 Hugo（extended，后台保存文章后在容器内重建） ----------
# 注意：官方 Hugo release 是 glibc 动态链接，这里必须用 Debian 而不是 Alpine
FROM debian:bookworm-slim AS hugo
ARG HUGO_VERSION=0.167.0
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget \
    && wget -qO /tmp/hugo.tgz "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-amd64.tar.gz" \
    && tar xzf /tmp/hugo.tgz -C /tmp \
    && mv /tmp/hugo /usr/local/bin/hugo \
    && chmod +x /usr/local/bin/hugo \
    && hugo version

# ---------- 运行镜像 ----------
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates tzdata gosu wget \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 1000 eblog && useradd -u 1000 -g eblog -m eblog

COPY --from=server /out/easyblog-server /usr/local/bin/easyblog-server
COPY --from=hugo /usr/local/bin/hugo /usr/local/bin/hugo

# 站点骨架分两层：
#   /app/site  首次启动种入 /data 的「你的数据」（hugo.yaml + 示例内容 + 友链）
#   /app/theme 每次启动同步进 /data 的「主题文件」（layouts/assets/static，随镜像升级）
# 注意：COPY 目录只拷内容，目录名要显式写在目标路径里
COPY hugo.yaml /app/site/
COPY content/ /app/site/content/
COPY data/ /app/site/data/
COPY layouts/ /app/theme/layouts/
COPY assets/ /app/theme/assets/
COPY static/ /app/theme/static/

COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh /usr/local/bin/easyblog-server /usr/local/bin/hugo

VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
