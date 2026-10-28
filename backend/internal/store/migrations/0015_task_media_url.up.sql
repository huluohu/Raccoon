-- 0015：任务的媒体直链与媒体域名（2026-10-28）。
--
-- media_url：解析阶段拿到的**真实媒体直链**（媒体 CDN，带签名有时效）。
-- media_host：它的主机名（如 googlevideo.com）。
--
-- 为什么存：解析打的是站点 API，下载打的是媒体 CDN——两者往往不是同一个域名。
-- 「解析成功但下载失败」十有八九出在媒体域名上（代理规则只覆盖了解析域名）。
-- 把域名摊开存下来，失败时才能点名说清「这条要走哪个 CDN」，而不是笼统的「网络超时」。
--
-- 只用于排查与展示：直链带签名且会过期，不做断点续传复用（过期后重解析即可）。
ALTER TABLE tasks ADD COLUMN media_url TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN media_host TEXT NOT NULL DEFAULT '';
