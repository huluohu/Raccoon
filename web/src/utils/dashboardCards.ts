// 仪表盘卡片注册表（docs/14 §3.2 复刻 17 块 + docs/15 §2.3 E1 的权威 key 清单）。
//
// 为什么单独一个文件：
// 1) E1「显示/隐藏 + 排序」要持久化 key，key 必须有唯一权威清单——
//    写死在视图里的话，卡片改名/换顺序时旧偏好会静默失效（用户会看到「设置没生效」）。
// 2) 取数与格式化（字节、时长、百分比）集中在这里，视图只负责布局，
//    这样「同一个数在两处显示不一致」不会发生。
// 3) 纯函数，好测：给定输入就能断言输出，不依赖组件挂载。
//
// 与原版 17 块的对应关系（docs/14 §1）：
//   1 快速下载卡 → 视图内 QuickSubmit 引导卡（不是指标卡）
//   2 运行时长 → uptime
//   3 总下载 → total
//   4 今日下载 → today
//   5 成功率 → success_rate
//   6 下载总量 → media_bytes
//   7 图片 → images
//   8 视频 → videos
//   9 本周下载 → week
//  10 订阅下载 → sub_downloads
//  11 收藏下载 → favorite_downloads
//  12 CPU → 监控带（donut）
//  13 内存 → 监控带（donut）
//  14 存储 → 监控带（donut）
//  15 网络带宽 → 监控带（数字）
//  16/17 7 天 / 30 天趋势 → 趋势卡（Tab 切换）
//  18 直播录制卡 → live_recording 卡 + 直播录制卡
import { formatBytes } from './format'
import type { SummaryStats, SystemInfo } from '@/api/types'

export type CardTone = 'neutral' | 'info' | 'success' | 'warning' | 'danger' | 'gold'

export interface DashboardCard {
  key: string
  label: string
  /** 已格式化好的值（格式化归本文件，卡片只显示）。 */
  value: string
  hint: string
  tone: CardTone
  /** 线稿图标 path（16×16）。 */
  icon: string
  /** 有值时整卡可点。 */
  to?: string
}

/** 成功率预警阈值（E2 默认值；配置中心 alert.success_rate_threshold 后续接入）。 */
export const DEFAULT_RATE_THRESHOLD = 80

export interface CardInput {
  summary: SummaryStats | null
  info: SystemInfo | null
  /** 失败任务数 + 凭据异常数（「待处理问题」卡的分子）。 */
  issueCount: number
  /** 正在录制的直播源数（summary 缺该字段时兜底）。 */
  recordingCount: number
}

/** 运行时长 → 人话（秒 → 秒/分/小时/天）。 */
export function formatUptime(sec: number): string {
  if (sec < 60) return `${sec} 秒`
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时 ${Math.floor((sec % 3600) / 60)} 分`
  return `${Math.floor(sec / 86400)} 天 ${Math.floor((sec % 86400) / 3600)} 小时`
}

/** 成功率 → 文案：null 显示「—」（分母 0 时不造 0%/100%）。 */
export function formatRate(rate: number | null | undefined): string {
  if (rate === null || rate === undefined) return '—'
  return `${rate.toFixed(1)}%`
}

/** 成功率 → tone：低于阈值切 danger（E2），无数据 neutral。 */
export function rateTone(rate: number | null | undefined, threshold = DEFAULT_RATE_THRESHOLD): CardTone {
  if (rate === null || rate === undefined) return 'neutral'
  return rate < threshold ? 'danger' : 'success'
}

/** 12 张指标卡的默认顺序（= 复刻原版的排列顺序，E1 只改显示与顺序）。 */
export const CARD_KEYS = [
  'uptime',
  'total',
  'today',
  'week',
  'success_rate',
  'media_bytes',
  'images',
  'videos',
  'sub_downloads',
  'favorite_downloads',
  'live_recording',
  'issues',
] as const

/** 卡片中文名（E1 面板里要显示人话，不能显示 key）。 */
export const CARD_LABELS: Record<string, string> = {
  uptime: '运行时长',
  total: '总下载',
  today: '今日下载',
  week: '本周下载',
  success_rate: '成功率',
  media_bytes: '下载总量',
  images: '图片',
  videos: '视频',
  sub_downloads: '订阅下载',
  favorite_downloads: '收藏下载',
  live_recording: '直播录制',
  issues: '待处理问题',
}

export function buildCards(input: CardInput): DashboardCard[] {
  const s = input.summary
  const kind = (k: string) => s?.media_by_kind?.[k] ?? 0
  const rate = s?.success_rate ?? null
  return [
    {
      key: 'uptime',
      label: '运行时长',
      value: formatUptime(s?.uptime_sec ?? input.info?.uptime ?? 0),
      hint: '本次启动至今',
      tone: 'neutral',
      icon: 'M8 4.2v4l2.6 1.6M8 1.8a6.2 6.2 0 1 0 0 12.4A6.2 6.2 0 0 0 8 1.8z',
    },
    {
      key: 'total',
      label: '总下载',
      value: String(s?.total_downloads ?? 0),
      hint: '累计任务数（不含已删）',
      tone: 'info',
      icon: 'M8 2.4v7.2m0 0 2.8-2.8M8 9.6 5.2 6.8M2.6 12.8h10.8',
    },
    {
      key: 'today',
      label: '今日下载',
      value: String(s?.today_downloads ?? 0),
      hint: '今天完成的',
      tone: 'success',
      icon: 'M3 8.4l3.2 3.2L13 5',
    },
    {
      key: 'week',
      label: '本周下载',
      value: String(s?.week_downloads ?? 0),
      hint: '周一为首日',
      tone: 'success',
      icon: 'M2.8 4.6h10.4v8.6H2.8zM2.8 7.2h10.4M5.6 2.6v3.2M10.4 2.6v3.2',
    },
    {
      key: 'success_rate',
      label: '成功率',
      value: formatRate(rate),
      hint: '完成 /（完成+失败）',
      tone: rateTone(rate),
      icon: 'M2.8 8.2l2.6 2.6 5-5.4M8 1.8a6.2 6.2 0 1 0 0 12.4A6.2 6.2 0 0 0 8 1.8z',
    },
    {
      key: 'media_bytes',
      label: '下载总量',
      value: formatBytes(s?.media_bytes ?? 0),
      hint: '媒体库占用',
      tone: 'info',
      icon: 'M2.6 3.4h4l1.2 1.6h5.6v7.6H2.6z',
    },
    {
      key: 'images',
      label: '图片',
      value: String(kind('image')),
      hint: '媒体库图片数',
      tone: 'neutral',
      icon: 'M2.6 3.6h10.8v8.8H2.6zM5.4 6.6a.9.9 0 1 0 0-.1M2.6 10.6l3-3 2.2 2.2 1.6-1.6 3.2 3.2',
    },
    {
      key: 'videos',
      label: '视频',
      value: String(kind('video')),
      hint: '媒体库视频数',
      tone: 'info',
      icon: 'M2.6 4.4h7.2v7.2H2.6zM9.8 7.2l3.6-2v5.6l-3.6-2z',
    },
    {
      key: 'sub_downloads',
      label: '订阅下载',
      value: String(s?.sub_downloads ?? 0),
      hint: '来自订阅的完成数',
      tone: 'warning',
      icon: 'M2.8 6.4a5.2 5.2 0 0 1 5.2 5.2M2.8 3.4a8.2 8.2 0 0 1 8.2 8.2M4.2 11.8a.9.9 0 1 0 0-.1',
    },
    {
      key: 'favorite_downloads',
      label: '收藏下载',
      value: String(s?.favorite_downloads ?? 0),
      hint: '收藏夹订阅来源（含已删订阅）',
      tone: 'gold',
      icon: 'M8 2.8l1.7 3.5 3.8.5-2.8 2.7.7 3.8L8 11.5 4.6 13.3l.7-3.8L2.5 6.8l3.8-.5z',
    },
    {
      key: 'live_recording',
      label: '直播录制',
      value: String(s?.live_recording ?? input.recordingCount),
      hint: '正在录制的源',
      tone: 'danger',
      icon: 'M8 9.2a1.6 1.6 0 1 0 0-3.2 1.6 1.6 0 0 0 0 3.2zM5 5a4.6 4.6 0 0 0 0 6m6 0a4.6 4.6 0 0 0 0-6',
      to: '/live',
    },
    {
      key: 'issues',
      label: '待处理问题',
      value: String(input.issueCount),
      hint: '失败任务 + 凭据异常',
      tone: input.issueCount > 0 ? 'danger' : 'neutral',
      icon: 'M8 2.6a5.4 5.4 0 1 0 0 10.8A5.4 5.4 0 0 0 8 2.6zM8 5.4v3.2M8 11.2a.7.7 0 1 0 0-.1',
      to: '/tasks',
    },
  ]
}

export interface LayoutPref {
  hidden: string[]
  order: string[]
}

/** 默认布局（= 全量 17 块，首次体验与原版对齐）。 */
export const DEFAULT_LAYOUT: LayoutPref = { hidden: [], order: [] }

/**
 * 按偏好排列卡片：hidden 过滤，order 里出现的按 order 排，未出现的按默认顺序补在后面。
 * 纯函数，E1 的排序语义全在这里，视图不重复实现。
 */
export function applyLayout(cards: DashboardCard[], layout: LayoutPref): DashboardCard[] {
  const hidden = new Set(layout.hidden)
  const kept = cards.filter((c) => !hidden.has(c.key))
  if (!layout.order.length) return kept
  const rank = new Map(layout.order.map((k, i) => [k, i]))
  return [...kept].sort((a, b) => {
    const ra = rank.has(a.key) ? rank.get(a.key)! : Number.MAX_SAFE_INTEGER
    const rb = rank.has(b.key) ? rank.get(b.key)! : Number.MAX_SAFE_INTEGER
    return ra - rb
  })
}
