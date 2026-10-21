// 前端通用工具：格式化（大小/速度/时间/时长）与状态文案。
// 铁律：数字一律等宽（tabular-nums）；未知显「未知」，不估算（docs/01 §5.3）。

/** 字节 → 可读大小（未知传 null → "未知"）。 */
export function formatBytes(n: number | null | undefined, fallback = '未知'): string {
  if (n === null || n === undefined || Number.isNaN(n)) return fallback
  if (n === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

/** 速度格式化。 */
export function formatSpeed(bps: number | null | undefined): string {
  if (!bps || bps <= 0) return '—'
  return `${formatBytes(bps)}/s`
}

/** Unix 毫秒 → 本地时间。 */
export function formatTime(ms: number | null | undefined, fallback = '—'): string {
  if (!ms) return fallback
  const d = new Date(ms)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** Unix 毫秒 → 相对时间（3 分钟前 / 2 小时前）。 */
export function formatRelative(ms: number | null | undefined): string {
  if (!ms) return '—'
  const diff = Date.now() - ms
  if (diff < 0) return formatTime(ms)
  const min = Math.floor(diff / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  const h = Math.floor(min / 60)
  if (h < 24) return `${h} 小时前`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d} 天前`
  return formatTime(ms)
}

/** 秒 → 时长（1:23:45）。 */
export function formatDuration(sec: number | null | undefined, fallback = '未知'): string {
  if (sec === null || sec === undefined || sec <= 0) return fallback
  const s = Math.floor(sec)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const r = s % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(r)}` : `${m}:${pad(r)}`
}

/** 剩余时间（ETA 秒）。 */
export function formatEta(sec: number | null | undefined): string {
  if (!sec || sec <= 0) return '—'
  if (sec < 60) return `${sec} 秒`
  return formatDuration(sec)
}

/** 七态中文文案（docs/01 §4 界面表达）。 */
export const STATUS_TEXT: Record<string, string> = {
  waiting: '等待中',
  parsing: '解析中',
  downloading: '下载中',
  post: '后处理',
  done: '已完成',
  failed: '失败',
  cancelled: '已取消',
}

/** 等待原因文案（wait_reason → 人话）。 */
export const WAIT_REASON_TEXT: Record<string, string> = {
  auto_retry: '等待自动重试',
  interrupted: '进程重启后重新排队',
  worker_panic: '内部异常后重新排队',
  need_login: '需要登录，请到连接页更新凭据',
  disk_check: '等待磁盘检查',
}

/** 状态 → 语义色 token 名（StatusDot 用；三重编码不只靠颜色）。 */
export const STATUS_TONE: Record<string, 'neutral' | 'info' | 'active' | 'success' | 'danger' | 'muted'> = {
  waiting: 'muted',
  parsing: 'info',
  downloading: 'active',
  post: 'info',
  done: 'success',
  failed: 'danger',
  cancelled: 'muted',
}

/** 平台 → 品牌色（PlatformBadge；禁 emoji 冒充）。 */
export const PLATFORM_TONE: Record<string, string> = {
  bilibili: '#FB7299',
  youtube: '#FF0000',
  weibo: '#E6162D',
  douyin: '#FE2C55',
  twitter: '#1DA1F2',
  xiaohongshu: '#FF2442',
  vimeo: '#1AB7EA',
  soundcloud: '#FF5500',
  bandcamp: '#629AA9',
  generic: 'var(--text-3)',
}

/** 平台展示名。 */
export const PLATFORM_TEXT: Record<string, string> = {
  bilibili: '哔哩哔哩',
  youtube: 'YouTube',
  weibo: '微博',
  douyin: '抖音',
  twitter: 'Twitter',
  xiaohongshu: '小红书',
  vimeo: 'Vimeo',
  soundcloud: 'SoundCloud',
  bandcamp: 'Bandcamp',
  web: '通用网页',
  batch: '批量',
  telegram: 'Telegram',
}

/** 平台展示名（未收录回退原值）。 */
export function platformText(p: string | undefined | null): string {
  if (!p) return '未知'
  return PLATFORM_TEXT[p] ?? p
}

/** 平台品牌色。 */
export function platformTone(p: string | undefined | null): string {
  if (!p) return 'var(--text-3)'
  return PLATFORM_TONE[p] ?? 'var(--text-3)'
}

/** 百分比（未知 → "—"）。 */
export function formatPercent(p: number | null | undefined): string {
  if (p === null || p === undefined || Number.isNaN(p)) return '—'
  return `${p.toFixed(0)}%`
}
