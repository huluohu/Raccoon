import { describe, expect, it } from 'vitest'
import { errorText } from '@/utils/format'
import { ApiError } from '@/api/types'

// 错误展示：后端错误信封带 code / message / detail 三段。
// 只显示 message 时，引擎问题会退化成一句「引擎退出码 1」——
// 用户既不知道该升级引擎、换链接还是补凭据，也没法自己排查。

describe('errorText', () => {
  it('无 detail 时只返回 message', () => {
    const e = new ApiError('VALIDATION', '视频不存在、已删除或链接有误', 'req-1')
    expect(errorText(e, '兜底')).toBe('视频不存在、已删除或链接有误')
  })

  it('有 detail 时把引擎原始原因带进文案', () => {
    const e = new ApiError(
      'VALIDATION',
      '视频不存在、已删除或链接有误',
      'req-1',
      '[youtube] aaaaaaaaaaa: This video is unavailable',
    )
    const text = errorText(e, '兜底')
    expect(text).toContain('视频不存在、已删除或链接有误')
    expect(text).toContain('This video is unavailable')
  })

  it('message 已包含 detail 时不重复拼接', () => {
    const e = new ApiError('VALIDATION', 'The page needs to be reloaded.', 'req-1', 'The page needs to be reloaded.')
    expect(errorText(e, '兜底')).toBe('The page needs to be reloaded.')
  })

  it('非 Error 对象走兜底文案', () => {
    expect(errorText('字符串', '兜底')).toBe('兜底')
    expect(errorText(null, '兜底')).toBe('兜底')
  })

  it('Error 但没有 message 时用兜底', () => {
    expect(errorText(new Error(''), '兜底')).toBe('兜底')
  })
})

// ---- waitReasonWithCountdown（重试等待期倒计时，docs/05 §7） ----

describe('waitReasonWithCountdown', () => {
  const importFmt = async () => (await import('@/utils/format')) as typeof import('@/utils/format')

  it('auto_retry 且未到点：显示「Ns 后自动重试」', async () => {
    const { waitReasonWithCountdown } = await importFmt()
    const now = 1_000_000
    const text = waitReasonWithCountdown(
      { status: 'waiting', wait_reason: 'auto_retry', scheduled_at: now + 30_500 },
      now,
    )
    expect(text).toBe('31 秒后自动重试')
  })

  it('到点未领取（worker 占满）：回退「等待自动重试」，不猜时刻', async () => {
    const { waitReasonWithCountdown } = await importFmt()
    const now = 1_000_000
    expect(
      waitReasonWithCountdown({ status: 'waiting', wait_reason: 'auto_retry', scheduled_at: now - 1 }, now),
    ).toBe('等待自动重试')
  })

  it('auto_retry 但无 scheduled_at（老数据）：回退基础文案', async () => {
    const { waitReasonWithCountdown } = await importFmt()
    expect(waitReasonWithCountdown({ status: 'waiting', wait_reason: 'auto_retry' }, 0)).toBe('等待自动重试')
  })

  it('非 waiting 或无 wait_reason：返回空串', async () => {
    const { waitReasonWithCountdown } = await importFmt()
    expect(waitReasonWithCountdown({ status: 'downloading' }, 0)).toBe('')
    expect(waitReasonWithCountdown({ status: 'waiting' }, 0)).toBe('')
  })

  it('其他等待原因原样走 WAIT_REASON_TEXT', async () => {
    const { waitReasonWithCountdown } = await importFmt()
    expect(
      waitReasonWithCountdown({ status: 'waiting', wait_reason: 'interrupted', scheduled_at: 999 }, 0),
    ).toBe('进程重启后重新排队')
  })
})
