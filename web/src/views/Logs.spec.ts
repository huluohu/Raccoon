import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { mountView } from '@/test/mount'
import Logs from '@/views/Logs.vue'
import { ApiError, type LogItem } from '@/api/types'
import { RButtonStub, REmptyStub } from '@/test/stubs'

/**
 * Logs.vue 用例（docs/15 T05 验收 #5）：
 * 过滤器（级别多选 / 关键词防抖）、cursor 分页（追加 + 到底判定）、
 * 脱敏行原样展示、E4 实时跟随的起停。
 *
 * 说明：RButton/REmpty 用仓里的通用 stub（渲染细节归各自 spec），
 * 其余全部真实渲染——这里要断言的就是视图自己的编排与请求参数。
 */

// vi.mock 的工厂会被提升到文件顶部，引用不到后面才初始化的 const，故先用 vi.hoisted 造 mock。
const mocks = vi.hoisted(() => ({ logs: vi.fn() }))

vi.mock('@/api/client', () => ({ api: mocks }))

// 视图里有定时器（跟随轮询 / 关键词防抖），漏卸载会让回调打到下一个用例的 DOM 上。
function item(over: Partial<LogItem> = {}): LogItem {
  return { ts: 1730000000, level: 'info', module: 'task', msg: '任务开始', ...over }
}

function page(items: LogItem[], next = '') {
  return { items, next_cursor: next }
}

/** 最近一次请求的参数（mock.calls 里是 [params] 单参）。 */
function lastParams(): Record<string, string> {
  return mocks.logs.mock.calls[mocks.logs.mock.calls.length - 1][0]
}

/** 按文案找按钮（RButton stub 渲染成普通 button）。 */
function btnByText(w: any, text: string) {
  return w.findAll('button').find((b: any) => b.text().includes(text))
}

beforeEach(() => {
  mocks.logs.mockReset().mockResolvedValue(page([]))
})

afterEach(() => {
  vi.useRealTimers()
})

describe('Logs 视图', () => {
  it('首屏按 limit=200 拉一页，并把时间/级别/module/msg 都渲染出来', async () => {
    mocks.logs.mockResolvedValueOnce(
      page([item({ level: 'warn', module: 'downloader', msg: '第 2 次重试，等待 5s' })]),
    )
    const w = mountView(Logs, [], { global: { stubs: { RButton: RButtonStub, REmpty: REmptyStub } } })
    await flushPromises()

    expect(mocks.logs).toHaveBeenCalledTimes(1)
    expect(lastParams()).toMatchObject({ limit: '200' })
    // 首屏没有游标：带上空 cursor 会让后端按「第 0 行之后」再算一遍，白跑
    expect(lastParams().cursor).toBeUndefined()

    const row = w.find('.log-row')
    expect(row.exists()).toBe(true)
    expect(row.find('.ts').text()).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/)
    // 选择器跟着组件走：级别胶囊是 .lv-badge（docs/07 §2 只许它用 --font-caption）
    expect(row.find('.lv-badge').text()).toBe('warn')
    expect(row.find('.mod').text()).toBe('downloader')
    expect(row.find('.msg').text()).toBe('第 2 次重试，等待 5s')
  })

  it('级别 chips 多选：选 warn+error 后 level 参数是逗号拼接', async () => {
    const w = mountView(Logs, [], { global: { stubs: { RButton: RButtonStub, REmpty: REmptyStub } } })
    await flushPromises()
    // 默认全不选 = 不限级别，不能下发空 level（后端会当成过滤成空集）
    expect(mocks.logs.mock.calls[0][0].level).toBeUndefined()

    const chip = (text: string) => w.findAll('.chip').find((c: any) => c.text() === text)!
    await chip('warn').trigger('click')
    await flushPromises()
    expect(lastParams().level).toBe('warn')

    await chip('error').trigger('click')
    await flushPromises()
    expect(lastParams().level).toBe('warn,error')

    // 再点一次取消：只剩 error
    await chip('warn').trigger('click')
    await flushPromises()
    expect(lastParams().level).toBe('error')
  })

  it('关键词防抖 300ms 后才带 q 参数（每敲一个字都请求会闪）', async () => {
    vi.useFakeTimers()
    const w = mountView(Logs, [], { global: { stubs: { RButton: RButtonStub, REmpty: REmptyStub } } })
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.logs).toHaveBeenCalledTimes(1)

    await w.find('#log-q').setValue('COOKIE')
    expect(mocks.logs).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(299)
    expect(mocks.logs).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.logs).toHaveBeenCalledTimes(2)
    expect(lastParams()).toMatchObject({ q: 'COOKIE' })
  })

  it('分页：next_cursor 前进时出现「加载更多」，点击带 cursor 且结果追加不覆盖', async () => {
    mocks.logs
      .mockResolvedValueOnce(page([item({ msg: '第一条' })], '2'))
      .mockResolvedValueOnce(page([item({ msg: '第二条' })], '3'))

    const w = mountView(Logs, [], { global: { stubs: { RButton: RButtonStub, REmpty: REmptyStub } } })
    await flushPromises()
    expect(w.findAll('.log-row').length).toBe(1)

    const more = btnByText(w, '加载更多')
    expect(more).toBeTruthy()
    await more!.trigger('click')
    await flushPromises()

    expect(lastParams()).toMatchObject({ cursor: '2', limit: '200' })
    expect(w.findAll('.log-row').length).toBe(2)
    // 追加语义：旧条目必须还在（整体替换会让用户「加载更多后前一条消失」）
    expect(w.text()).toContain('第一条')
    expect(w.text()).toContain('第二条')
  })

  it('分页到底：next_cursor 与请求的 cursor 相同 → 隐藏按钮并提示「没有更多日志」', async () => {
    mocks.logs
      .mockResolvedValueOnce(page([item({ msg: '第一条' })], '2'))
      // 服务端约定：没有下一页时原样返回请求的 cursor
      .mockResolvedValueOnce(page([item({ msg: '第二条' })], '2'))

    const w = mountView(Logs, [], { global: { stubs: { RButton: RButtonStub, REmpty: REmptyStub } } })
    await flushPromises()
    await btnByText(w, '加载更多')!.trigger('click')
    await flushPromises()

    expect(btnByText(w, '加载更多')).toBeUndefined()
    expect(w.text()).toContain('没有更多日志')
    expect(w.findAll('.log-row').length).toBe(2)
  })

  it('脱敏行原样展示：msg 里的 *** 不被还原、不被二次加工', async () => {
    // 后端已把 cookie/token/password 的值换成 ***（docs/15 §6.3 只有一套脱敏）。
    // 前端任何「补全」都是把密钥写回页面，所以断言渲染文本与接口返回**逐字相同**。
    const raw = 'login failed: password=*** token=*** (user=bob)'
    mocks.logs.mockResolvedValueOnce(page([item({ level: 'error', module: 'auth', msg: raw })], ''))

    const w = mountView(Logs, [], { global: { stubs: { RButton: RButtonStub, REmpty: REmptyStub } } })
    await flushPromises()

    expect(w.find('.msg').text()).toBe(raw)
    expect(w.text()).toContain('***')
  })

  it('E4 实时跟随：打开后每 3 秒拉一次最新页，关闭后不再请求', async () => {
    vi.useFakeTimers()
    const w = mountView(Logs, [], { global: { stubs: { RButton: RButtonStub, REmpty: REmptyStub } } })
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.logs).toHaveBeenCalledTimes(1)

    await w.find('.follow input').setValue(true)
    await vi.advanceTimersByTimeAsync(0)
    // 打开即拉一次，不让用户等第一拍
    expect(mocks.logs).toHaveBeenCalledTimes(2)
    expect(w.find('.follow-state').text()).toContain('跟随中')
    // 跟随拉的是「最新一页」：不能带 cursor，否则拿到的是历史而不是新日志
    expect(lastParams().cursor).toBeUndefined()

    await vi.advanceTimersByTimeAsync(3000)
    expect(mocks.logs).toHaveBeenCalledTimes(3)

    await w.find('.follow input').setValue(false)
    await vi.advanceTimersByTimeAsync(9000)
    expect(mocks.logs).toHaveBeenCalledTimes(3)
    expect(w.find('.follow-state').text()).toContain('已暂停')
  })

  it('接口失败显示错误条与重试按钮，不白屏', async () => {
    mocks.logs.mockRejectedValueOnce(new ApiError('INTERNAL', '日志文件读不出来', 'req-1'))
    const w = mountView(Logs, [], { global: { stubs: { RButton: RButtonStub, REmpty: REmptyStub } } })
    await flushPromises()

    expect(w.find('.err').text()).toContain('日志文件读不出来')
    expect(btnByText(w, '重试')).toBeTruthy()

    // 点重试要真的再打一次接口（错误条不是死胡同）
    mocks.logs.mockResolvedValueOnce(page([item({ msg: '恢复后的日志' })], ''))
    await btnByText(w, '重试')!.trigger('click')
    await flushPromises()
    expect(mocks.logs).toHaveBeenCalledTimes(2)
    expect(w.find('.err').exists()).toBe(false)
    expect(w.text()).toContain('恢复后的日志')
  })
})
