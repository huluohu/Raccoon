import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SourceList from '@/components/live/SourceList.vue'
import { RButtonStub, PlatformBadgeStub } from '@/test/stubs'
import type { LiveSource } from '@/api/types'

/**
 * SourceList 用例（docs/07 §3「/live → SourceList」）。
 *
 * 关键验收点：
 * 1) 直播状态是「色点 + 文字」双重编码（§4 禁颜色唯一载体），
 *    色点是 aria-hidden 的纯装饰，信息在文字里。
 * 2) 录制计划徽章要翻译成人话（仅手动 / 开播即录 / 定时录制）。
 * 3) 正在录制的源不允许再点「开始录制」（按钮置灰 + 文案改成「录制中」）。
 * 4) 四个动作各自抛独立事件。
 * 5) 停用的源降透明度。
 */
const stubs = {
  RButton: RButtonStub,
  PlatformBadge: PlatformBadgeStub,
}

function makeSrc(over: Partial<LiveSource> = {}): LiveSource {
  return {
    id: 'l-1',
    platform: 'bilibili',
    room_id: '12345',
    room_url: 'https://live.bilibili.com/12345',
    title: '晚间场',
    enabled: true,
    quality: 'best',
    record_plan: 'manual',
    segment_min: 30,
    check_interval_sec: 120,
    status: 'offline',
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
    recording_count: 3,
    ...over,
  }
}

describe('SourceList 组件', () => {
  it('渲染平台徽章、状态文字与录制计划徽章', () => {
    const w = mount(SourceList, { props: { items: [makeSrc()] }, global: { stubs } })
    expect(w.find('.stub-badge').attributes('data-platform')).toBe('bilibili')
    expect(w.find('.status').text()).toContain('未开播')
    expect(w.find('.plan').text()).toBe('仅手动')
    expect(w.find('.card-title').text()).toBe('晚间场')
  })

  it('状态色点 + 文字双重编码：色点是纯装饰，信息在文字里', () => {
    const w = mount(SourceList, {
      props: { items: [makeSrc({ status: 'live' })] },
      global: { stubs },
    })
    const dot = w.find('.dot')
    expect(dot.attributes('aria-hidden')).toBe('true')
    expect(w.find('.status').text()).toContain('直播中')
    expect(w.find('.status').attributes('style')).toContain('--success')
  })

  it('四种直播状态各有文案（不靠颜色区分）', () => {
    const map: Array<[LiveSource['status'], string]> = [
      ['offline', '未开播'],
      ['live', '直播中'],
      ['recording', '录制中'],
      ['error', '异常'],
    ]
    for (const [status, text] of map) {
      const w = mount(SourceList, { props: { items: [makeSrc({ status })] }, global: { stubs } })
      expect(w.find('.status').text()).toContain(text)
    }
  })

  it('元信息包含房间号/画质/分段/检测间隔/录制次数', () => {
    const w = mount(SourceList, { props: { items: [makeSrc()] }, global: { stubs } })
    const text = w.text()
    expect(text).toContain('12345')
    expect(text).toContain('30 分钟')
    expect(text).toContain('每 120s')
    expect(text).toContain('3')
  })

  it('画质留空时回落 best', () => {
    const w = mount(SourceList, {
      props: { items: [makeSrc({ quality: undefined })] },
      global: { stubs },
    })
    expect(w.text()).toContain('best')
  })

  it('四个动作各自抛独立事件（带直播源对象）', async () => {
    const src = makeSrc()
    const w = mount(SourceList, { props: { items: [src] }, global: { stubs } })
    const btns = w.findAll('button')
    const find = (label: string) => btns.find((b) => b.text().includes(label))!

    await find('开始录制').trigger('click')
    expect(w.emitted('record')![0]).toEqual([src])

    await find('录制历史').trigger('click')
    expect(w.emitted('history')![0]).toEqual([src])

    await find('停止监控').trigger('click')
    expect(w.emitted('toggle')![0]).toEqual([src])

    await find('删除').trigger('click')
    expect(w.emitted('remove')![0]).toEqual([src])
  })

  it('正在录制的源：开始录制按钮置灰且文案是「录制中」', () => {
    const w = mount(SourceList, {
      props: { items: [makeSrc({ status: 'recording' })] },
      global: { stubs },
    })
    const btn = w.findAll('button').find((b) => b.text().includes('录制中'))!
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('提交中的源：文案是「提交中…」并置灰', () => {
    const w = mount(SourceList, {
      props: { items: [makeSrc()], recordingId: 'l-1' },
      global: { stubs },
    })
    const btn = w.findAll('button').find((b) => b.text().includes('提交中'))!
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('停用的源显示「恢复监控」并降透明度', () => {
    const w = mount(SourceList, {
      props: { items: [makeSrc({ enabled: false })] },
      global: { stubs },
    })
    expect(w.findAll('button').some((b) => b.text().includes('恢复监控'))).toBe(true)
    expect(w.find('.card').classes()).toContain('off')
  })

  it('有错误说明时显示错误文案', () => {
    const w = mount(SourceList, {
      props: { items: [makeSrc({ error_note: '房间不存在' })] },
      global: { stubs },
    })
    expect(w.find('.err-note').text()).toBe('房间不存在')
  })
})
