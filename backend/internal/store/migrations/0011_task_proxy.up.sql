-- 0011：任务指定网络代理（docs/05 §4 任务网络出口）。
-- proxy 空串 = 遵循系统默认（直连或配置中心的 net.proxy）；
-- 填值（如 http://... 或 socks5://...）= 该任务独立使用指定代理，
-- 并在直连网络异常时支持自动或手动切换此代理。
ALTER TABLE tasks ADD COLUMN proxy TEXT NOT NULL DEFAULT '';
