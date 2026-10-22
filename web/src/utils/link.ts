// link.ts：链接格式校验（提交前的第一道闸）。
//
// 为什么需要这道闸：
// 后端 validateOutboundURL 只做「协议白名单 + 主机名 + 内网拒绝」，
// 失败时返回的是技术口径的文案（"仅支持 http/https 链接"）。
// 用户从 B 站/抖音分享出来的是一整段带文案的文本：
//   「【标题】https://b23.tv/abc 复制打开bilibili」
// 直接粘进来，后端会拒，用户看到的是一句看不懂的协议报错。
//
// 这里在前端提交前做三件事：
// 1) 从任意文本里**提取**出真正的链接（分享文案场景最常见）；
// 2) 校验提取结果是不是 http/https、有没有主机名；
// 3) 把「错在哪」翻译成人话，指清下一步该干什么。
//
// 铁律（docs/07 §8）：只做格式层的判断，不猜平台、不猜可下载性——
// 「这个链接能不能下」是引擎的事，前端不冒充。

/** 提取结果。 */
export interface LinkCheck {
  /** 提取到的链接（没提取到时为空串）。 */
  url: string
  /** 是否可以提交。 */
  ok: boolean
  /** 不能提交时的人话提示（能提交时为空串）。 */
  message: string
}

/**
 * URL 匹配：协议头 + 主机名，够用且不引入歧义。
 * 主机名允许为空（`https://` 这种残缺输入也要能匹配到），
 * 由后面的主机名检查给出更准确的提示。
 */
const URL_RE = /https?:\/\/[^\s"'<>，。；、）】]*/i

/**
 * 从任意文本里提取第一个 http/https 链接。
 *
 * 分享文案的常见形态：
 *   - 「【标题】https://b23.tv/abc 复制打开bilibili」
 *   - 「3.14 复制打开抖音，看看…… https://v.douyin.com/xyz/」
 *   - 「https://www.bilibili.com/video/BV1xx411c7mD」
 *
 * 结尾的标点（中文句读、英文括号）会被剔除——它们是文案的一部分，
 * 不是链接的一部分，带进去会让引擎拿到一个 404 的路径。
 */
export function extractUrl(text: string): string {
  const m = URL_RE.exec(text.trim())
  if (!m) return ''
  // 去掉末尾的中英文标点与右括号（分享文案里最常见的粘连）。
  return m[0].replace(/[)）】\]}>，。；、！!？?"'』」]+$/, '')
}

/**
 * 校验输入里的链接是否可以提交。
 *
 * @param text 用户输入的原文（可能是纯链接，也可能是分享文案）
 */
export function checkLink(text: string): LinkCheck {
  const trimmed = text.trim()
  if (!trimmed) {
    return { url: '', ok: false, message: '先粘贴一个链接吧' }
  }

  // 文本里有链接就提取；一个都没有但看着像个地址（缺协议头）时，
  // 给一条明确的提示而不是「格式错误」——缺协议头是最常见的输错。
  const url = extractUrl(trimmed)
  if (!url) {
    if (/^\s*[\w.-]+\.[a-z]{2,}(\/|\?|$)/i.test(trimmed)) {
      return {
        url: '',
        ok: false,
        message: '这个地址少了 http:// 或 https:// 开头，补上再提交',
      }
    }
    return {
      url: '',
      ok: false,
      message: '没找到链接。把视频页面地址复制完整粘进来（带分享文案也行）',
    }
  }

  // 提取成功后再校验协议与主机名。
  if (!/^https?:\/\//i.test(url)) {
    return { url, ok: false, message: '只支持 http/https 链接' }
  }
  const host = url.replace(/^https?:\/\//i, '').split(/[/?#]/)[0]
  if (!host) {
    return { url, ok: false, message: '这个链接缺少主机名，检查一下有没有复制全' }
  }
  return { url, ok: true, message: '' }
}
