# syntax=docker/dockerfile:1

# ---- 阶段 1：前端构建 ----
FROM node:20-bookworm-slim AS web-build
WORKDIR /w
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- 阶段 2：Go 构建（纯 Go 无 cgo，交叉编译友好）----
FROM golang:1.22-bookworm AS go-build
# TARGETOS/TARGETARCH 由 BuildKit 按目标平台自动注入（docs/dockerfile 自动平台参数）。
# ⚠️ 不能写默认值（如 ARG TARGETARCH=amd64）：阶段内带默认值的重声明会压过
# 平台注入值，多架构构建时 arm64 镜像里会被编进 amd64 二进制（exec format error，
# 2026-10-24 实测踩过）。不带默认值时若为空，go 回退构建容器本机平台，语义安全。
ARG TARGETOS
ARG TARGETARCH
# 应用版本（打进 server.version；发布命令见 Makefile docker-release / docs/17）。
ARG VERSION=1.0.0
WORKDIR /src
COPY backend/ ./
# 前端产物进包（embed.FS，ADR-5）
COPY --from=web-build /w/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildvcs=false -trimpath -ldflags "-s -w -X github.com/huluohu/raccoon/internal/server.version=${VERSION}" \
    -o /out/raccoon ./cmd/raccoon

# ---- 阶段 3：运行镜像（非 root，内置引擎）----
FROM debian:bookworm-slim
ARG YTDLP_VERSION=2026.10.21
ARG GALLERYDL_VERSION=1.32.17
ARG CURLCFFI_VERSION=0.16.3
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl ffmpeg tzdata python3 python3-pip \
    && rm -rf /var/lib/apt/lists/* \
    # yt-dlp 固定版本（升级引擎=升级镜像，docs/02 §8）
    && curl -fsSL -o /usr/local/bin/yt-dlp \
      "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp" \
    && chmod 0755 /usr/local/bin/yt-dlp \
    # gallery-dl 固定版本（图片/图集站点引擎；pip 装进系统 python3，
    # 构建期以 root 安装、运行期降级 raccoon 用户只读使用）
    && pip3 install --no-cache-dir --break-system-packages "gallery-dl==${GALLERYDL_VERSION}" \
    && gallery-dl --version \
    # curl_cffi：TLS 指纹伪装。部分站点（PornHub 等）对非浏览器 TLS 指纹直接
    # 返回 410 Gone，yt-dlp 提示 "The extractor is attempting impersonation,
    # but no impersonate target is available"。装上后 yt-dlp 会自动走 impersonation。
    # 固定版本随镜像走，升级需重建镜像。
    && pip3 install --no-cache-dir --break-system-packages "curl_cffi==${CURLCFFI_VERSION}" \
    && python3 -c "import curl_cffi; print('curl_cffi', curl_cffi.__version__)" \
    # mutagen：yt-dlp --embed-metadata 写 mp4/m4a 标签时用它（mkv/webm 走 ffmpeg）。
    # 没它时 mp4 元数据内嵌会以后处理器告警失败。
    && pip3 install --no-cache-dir --break-system-packages "mutagen==1.47.0" \
    && python3 -c "import mutagen; print('mutagen', mutagen.version_string)"

COPY --from=go-build /out/raccoon /usr/local/bin/raccoon
# 非 root 运行（方案 §7.1）
RUN useradd --system --uid 1000 --create-home --home-dir /data raccoon
USER raccoon
WORKDIR /data
# 下载文件独立于数据目录：数据库/凭据/配置（/data）与视频文件（/downloads）分开挂载，
# 备份小体积的数据目录时不必背着媒体文件。路径由 env 锁定（设置页该项只读并标注来源），
# 挂载点由部署方决定——不挂载时文件落在容器可写层，容器删除即丢失，务必显式挂载。
ENV RACCOON_DATA_DIR=/data RACCOON_DOWNLOAD_DIR=/downloads RACCOON_SERVER_ADDR=:8530
EXPOSE 8530
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8530/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/raccoon"]
