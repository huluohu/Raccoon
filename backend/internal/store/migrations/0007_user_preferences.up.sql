-- 0007：用户偏好单行表（仪表盘布局 E1；单用户假设，CHECK 兜底；
-- 未来多用户时加列走只增迁移，不改本文件——checksum 防篡改）。
CREATE TABLE IF NOT EXISTS user_preferences (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    layout     TEXT NOT NULL DEFAULT '{"hidden":[],"order":[]}',
    updated_at INTEGER NOT NULL
);
