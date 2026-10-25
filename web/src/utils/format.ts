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

/**
 * 等待原因文案（带重试倒计时）：auto_retry 且已知退避结束点时显示「Ns 后自动重试」，
 * 到点后（调度器可能因 worker 占满稍晚领取）回退「等待自动重试」，不猜具体时刻。
 * 其余等待原因原样走 WAIT_REASON_TEXT。
 */
export function waitReasonWithCountdown(
  t: { status: string; wait_reason?: string; scheduled_at?: number | null },
  nowMs: number,
): string {
  if (t.status !== 'waiting' || !t.wait_reason) return ''
  if (t.wait_reason === 'auto_retry' && t.scheduled_at) {
    const sec = Math.ceil((t.scheduled_at - nowMs) / 1000)
    if (sec > 0) return `${sec} 秒后自动重试`
  }
  return WAIT_REASON_TEXT[t.wait_reason] ?? ''
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

/** 平台 → 品牌色（PlatformBadge；禁 emoji 冒充）。
 *  品牌色是外部客观事实（不是本项目的设计选择），所以不塞进 tokens.css。
 *  但品牌色块上的文字色必须逐个配对——天蓝 (#1AB7EA)、青灰 (#629AA9) 这类
 *  浅色压白字只有 2.4~2.9:1，达不到 WCAG AA 的 4.5:1。 */
export const PLATFORM_TONE: Record<string, string> = {
  bilibili: '#FB7299',
  youtube: '#FF0000',
  weibo: '#E6162D',
  douyin: '#FE2C55',
  tiktok: '#010101',
  twitter: '#1DA1F2',
  xiaohongshu: '#FF2442',
  vimeo: '#1AB7EA',
  soundcloud: '#FF5500',
  bandcamp: '#629AA9',
  pornhub: '#E7462F',
  netease: '#C62F2F',
  wechat: '#07C160',
  generic: 'var(--text-3)',
}

/** 平台品牌色块上的文字色（与 PLATFORM_TONE 逐个配对）。
 *  浅品牌（vimeo 天蓝、bandcamp 青灰、twitter 亮蓝）用深墨字，
 *  深品牌（youtube 红、tiktok 黑）用白字。 */
export const PLATFORM_TONE_FG: Record<string, string> = {
  vimeo: '#08243A',
  bandcamp: '#0D1F26',
  twitter: '#08243A',
  generic: '#FFFFFF',
}

/** 平台展示名。 */
export const PLATFORM_TEXT: Record<string, string> = {
  bilibili: '哔哩哔哩',
  youtube: 'YouTube',
  weibo: '微博',
  douyin: '抖音',
  tiktok: 'TikTok',
  twitter: 'Twitter',
  xiaohongshu: '小红书',
  vimeo: 'Vimeo',
  soundcloud: 'SoundCloud',
  bandcamp: 'Bandcamp',
  pornhub: 'PornHub',
  netease: '网易云音乐',
  wechat: '微信视频号',
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

/** 平台品牌色块上的文字色（与 platformTone 配对，保证 WCAG AA）。 */
export function platformToneFg(p: string | undefined | null): string {
  if (!p) return '#FFFFFF'
  return PLATFORM_TONE_FG[p] ?? '#FFFFFF'
}

/** 百分比（未知 → "—"）。 */
export function formatPercent(p: number | null | undefined): string {
  if (p === null || p === undefined || Number.isNaN(p)) return '—'
  return `${p.toFixed(0)}%`
}

/**
 * 错误 → 展示文案。
 *
 * 后端错误信封带三段信息：code（业务码）/ message（可展示文案）/ detail（引擎原始原因）。
 * 只显示 message 时，引擎问题会退化成一句「引擎退出码 1」——用户看不出该升级引擎、
 * 换链接还是补凭据。这里把 detail 拼进文案，detail 本身是英文原句，
 * 追加在中文提示后面，帮助判断又不喧宾夺主。
 */
export function errorText(e: unknown, fallback: string): string {
  if (!(e instanceof Error)) return fallback
  const base = e.message || fallback
  const detail = (e as { detail?: string }).detail
  if (!detail) return base
  // detail 已被 message 覆盖时不重复展示。
  if (base.includes(detail)) return base
  return `${base}（引擎原始信息：${detail}）`
}
