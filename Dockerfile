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
# Go 依赖单独一层：依赖文件不变时，改任何业务代码都不会重新下载模块
# （否则 COPY backend/ ./ 一变，整层的模块缓存就没了）。
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# 前端产物进包（embed.FS，ADR-5）
COPY --from=web-build /w/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildvcs=false -trimpath -ldflags "-s -w -X github.com/huluohu/raccoon/internal/server.version=${VERSION}" \
    -o /out/raccoon ./cmd/raccoon

# ---- 阶段 3：运行镜像（非 root，内置引擎）----
FROM debian:bookworm-slim
#
# 缓存友好性（2026-10-28 实测修正）：
# BuildKit 里 ARG 会让**它之后的所有层**失效。所以版本号 ARG 必须紧挨着
# 用到它的那一层声明——声明在阶段顶部时，改一个 yt-dlp 版本号会让
# apt + ffmpeg + 三个 pip 全部重下（实测 160s / 几百 MB）。
# 下面每个 ARG 只服务于紧随其后的那一个 RUN，其余层不受影响。
#
# 引擎各装各的层：以前 apt + ffmpeg + 三个 pip 挤在同一个 RUN 里，
# 升级任何一个引擎都会让整层失效 → 每次发版都重下几百 MB。
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl tzdata python3 python3-pip tar xz-utils \
    && rm -rf /var/lib/apt/lists/*

# 静态 ffmpeg：按目标架构选包（amd64→linux64 / arm64→linuxarm64）
# ffmpeg 用 yt-dlp 社区的静态构建而非 Debian 包（2026-10-25 瘦身）：
# Debian 的 ffmpeg 会拖入 libgl1→mesa→libllvm15（109MB）+libz3（21MB）+mesa-dri（23MB)
# 一整条 GPU 依赖链（容器内无用，且 purge 会连带删掉 ffmpeg 本身）。
# 静态构建单二进制含全部编解码器（h264/mp3lame/aac），bin/ 里取 ffmpeg+ffprobe 两个文件。
ARG TARGETARCH
ARG FFMPEG_TAG=latest
RUN case "${TARGETARCH}" in \
         amd64) FF_AR=linux64 ;; \
         arm64) FF_AR=linuxarm64 ;; \
         *) echo "不支持的架构: ${TARGETARCH}"; exit 1 ;; \
       esac \
    # 资产名里也带 tag 段（tag=latest → ffmpeg-master-latest-linux64-gpl.tar.xz）
    && curl -fsSL -o /tmp/ff.tar.xz \
      "https://github.com/yt-dlp/FFmpeg-Builds/releases/download/${FFMPEG_TAG}/ffmpeg-master-${FFMPEG_TAG}-${FF_AR}-gpl.tar.xz" \
    && mkdir -p /tmp/ff \
    && tar -xJf /tmp/ff.tar.xz -C /tmp/ff --strip-components=2 --wildcards '*/bin/ffmpeg' '*/bin/ffprobe' \
    && mv /tmp/ff/ffmpeg /tmp/ff/ffprobe /usr/local/bin/ \
    && rm -rf /tmp/ff /tmp/ff.tar.xz \
    && ffmpeg -version | head -1

# Python 引擎三个装在同一层：它们互不依赖、且都是几十 MB 级的 pip 包，
# 合在一起升级任一个版本只会重来这一层（而不是三层链式重跑）。
# gallery-dl：图片/图集站点引擎；pip 装进系统 python3，构建期以 root 安装、
# 运行期降级 raccoon 用户只读使用。
# curl_cffi：TLS 指纹伪装。部分站点（PornHub 等）对非浏览器 TLS 指纹直接
# 返回 410 Gone，yt-dlp 提示 "The extractor is attempting impersonation,
# but no impersonate target is available"。装上后 yt-dlp 会自动走 impersonation。
# mutagen：yt-dlp --embed-metadata 写 mp4/m4a 标签时用它（mkv/webm 走 ffmpeg）。
# 没它时 mp4 元数据内嵌会以后处理器告警失败。
ARG GALLERYDL_VERSION=1.32.17
ARG CURLCFFI_VERSION=0.16.3
RUN pip3 install --no-cache-dir --break-system-packages "gallery-dl==${GALLERYDL_VERSION}" \
    && gallery-dl --version \
    && pip3 install --no-cache-dir --break-system-packages "curl_cffi==${CURLCFFI_VERSION}" \
    && python3 -c "import curl_cffi; print('curl_cffi', curl_cffi.__version__)" \
    && pip3 install --no-cache-dir --break-system-packages "mutagen==1.47.0" \
    && python3 -c "import mutagen; print('mutagen', mutagen.version_string)" \
    # PySocks：gallery-dl 的 socks5 代理要靠它（底层是 requests，没装 PySocks 时
    # 配 socks5:// 会直接 InvalidSchema 失败——同一份 net.proxy 在 yt-dlp 上正常，
    # 在图片站任务上却挂，2026-10-28 定位）
    && pip3 install --no-cache-dir --break-system-packages "PySocks==1.7.1" \
    && python3 -c "import socks; print('PySocks ok')"

# yt-dlp 固定版本（升级引擎=升级镜像，docs/02 §8）。
# 放在最后：层是链式的，改这一层只会让**它之后**的层失效——
# yt-dlp 是发版时最常升级的引擎（单文件 3MB、5 秒），放末尾最省。
ARG YTDLP_VERSION=2026.10.21
RUN curl -fsSL -o /usr/local/bin/yt-dlp \
      "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp" \
    && chmod 0755 /usr/local/bin/yt-dlp \
    && yt-dlp --version

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
