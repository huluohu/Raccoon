-- 0005: 会话持久化（auth_sessions）。
-- 背景：内存会话表在应用重启后清空，浏览器里仍有效的 raccoon_session Cookie
-- 立刻变废纸，用户被迫重新登录。会话落 SQLite（docs/03 数据模型同源），
-- 重启后由同一张表恢复校验，登录态跨重启保持。
-- remember 标记区分两种有效期：1 = 「30 天内免登录」（30 天滑动窗口），
-- 0 = 普通会话（24h 滑动窗口，Cookie 不带 Max-Age，浏览器关闭即失效）。
CREATE TABLE IF NOT EXISTS auth_sessions (
    sid        TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    remember   INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_last_seen ON auth_sessions(last_seen);
