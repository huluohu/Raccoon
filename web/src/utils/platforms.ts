// 平台清单：内容平台（下载源账号）与集成组件（系统连接）的归属边界。
//
// 为什么要有这份清单：凭据中心一张表存两类东西——B站/YouTube 的登录态
// （内容平台账号，按任务自动注入引擎）和 Telegram/115 的服务凭据（集成组件，
// 连的是「外部系统」不是「内容源」）。UI 上它们分属两个页面
// （「平台账号」/「连接」），判定归属需要同一份集合，前后端口径一致。

/** 内容平台：下载源的账号登录态（扫码 / 粘贴 Cookie），任务执行时注入引擎。 */
export const CONTENT_PLATFORMS = [
  'bilibili',
  'youtube',
  'douyin',
  'tiktok',
  'twitter',
  'instagram',
  'youku',
  'tencent',
  'iqiyi',
  'weibo',
  'xiaohongshu',
  'netease',
  'zhihu',
  'douyu',
  'wechat',
] as const

/** 集成组件：与外部系统的连接凭据（Telegram Bot、115、qBittorrent）。 */
export const INTEGRATION_PLATFORMS = ['telegram', 'n115', 'qbittorrent'] as const

export function isContentPlatform(platform: string): boolean {
  return (CONTENT_PLATFORMS as readonly string[]).includes(platform)
}

/** 内容平台的展示名（平台账号页/徽章用）。 */
export const PLATFORM_LABELS: Record<string, string> = {
  bilibili: '哔哩哔哩',
  youtube: 'YouTube',
  douyin: '抖音',
  tiktok: 'TikTok',
  twitter: 'X（Twitter）',
  instagram: 'Instagram',
  youku: '优酷',
  tencent: '腾讯视频',
  iqiyi: '爱奇艺',
  weibo: '微博',
  xiaohongshu: '小红书',
  netease: '网易云音乐',
  zhihu: '知乎',
  douyu: '斗鱼',
  wechat: '微信视频号（腾讯元宝）',
}
