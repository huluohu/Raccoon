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
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY backend/ ./
# 前端产物进包（embed.FS，ADR-5）
COPY --from=web-build /w/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildvcs=false -trimpath -ldflags "-s -w -X github.com/huluohu/raccoon/internal/server.version=0.1.0" \
    -o /out/raccoon ./cmd/raccoon

# ---- 阶段 3：运行镜像（非 root，内置引擎）----
FROM debian:bookworm-slim
ARG YTDLP_VERSION=2025.01.15
ARG GALLERYDL_VERSION=1.32.17
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
    && gallery-dl --version

COPY --from=go-build /out/raccoon /usr/local/bin/raccoon
# 非 root 运行（方案 §7.1）
RUN useradd --system --uid 1000 --create-home --home-dir /data raccoon
USER raccoon
WORKDIR /data
ENV RACCOON_DATA_DIR=/data RACCOON_SERVER_ADDR=:8530
EXPOSE 8530
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8530/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/raccoon"]
