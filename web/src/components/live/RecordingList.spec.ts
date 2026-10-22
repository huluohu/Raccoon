import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import RecordingList from '@/components/live/RecordingList.vue'

/**
 * RecordingList 用例（docs/07 §3「/live → RecordingList」）。
 *
 * 关键验收点：
 * 1) 表头 caption(12px) / 表体 body(16px) 的字号规则和表结构绑在一起——
 *    这里断言的是表结构本身：thead 五列、tbody 行。
 * 2) 状态文字 + 颜色成对（§4：禁颜色唯一载体）。
 * 3) 没有结束时间的录制显示「进行中」，不显示成 0:00（那是在骗人）。
 * 4) 空列表显示引导文案，不留一张空表。
 */
function makeRecording(over: Partial<import('@/api/types').LiveRecording> = {}) {
  return {
    id: 'r-1',
    source_id: 'l-1',
    started_at: 1_700_000_000_000,
    ended_at: 1_700_000_000_000 + 3661 * 1000,
    segments: 3,
    size: 100 * 1024 * 1024,
    status: 'completed' as const,
    ...over,
  }
}

describe('RecordingList 组件', () => {
  it('汇总行给出次数/分段数/总体积', () => {
    const w = mount(RecordingList, {
      props: {
        recordings: [makeRecording()],
        totalSegments: 3,
        totalSize: 100 * 1024 * 1024,
      },
    })
    expect(w.find('.hist-sum').text()).toContain('共 1 次录制')
    expect(w.find('.hist-sum').text()).toContain('3 个分段')
    expect(w.find('.hist-sum').text()).toContain('100.0 MB')
  })

  it('表头五列分明', () => {
    const w = mount(RecordingList, { props: { recordings: [makeRecording()] } })
    const headers = w.findAll('thead th').map((th) => th.text())
    expect(headers).toEqual(['开始', '时长', '分段', '大小', '状态'])
    expect(w.findAll('tbody tr').length).toBe(1)
  })

  it('已完成显示时长；进行中显示「进行中」而不是 0:00', () => {
    const w = mount(RecordingList, {
      props: {
        recordings: [makeRecording(), makeRecording({ id: 'r-2', ended_at: undefined, status: 'recording' })],
      },
    })
    const cells = w.findAll('tbody tr')[0].findAll('td')
    expect(cells[1].text()).toBe('1:01:01')
    const cells2 = w.findAll('tbody tr')[1].findAll('td')
    expect(cells2[1].text()).toBe('进行中')
  })

  it('状态文字与颜色成对出现', () => {
    const w = mount(RecordingList, {
      props: {
        recordings: [
          makeRecording({ id: 'r-1', status: 'completed' }),
          makeRecording({ id: 'r-2', status: 'failed' }),
          makeRecording({ id: 'r-3', status: 'recording' }),
        ],
      },
    })
    const cells = w.findAll('tbody tr')
    expect(cells[0].findAll('td')[4].text()).toBe('已完成')
    expect(cells[0].findAll('td')[4].attributes('style')).toContain('--success')
    expect(cells[1].findAll('td')[4].text()).toBe('失败')
    expect(cells[1].findAll('td')[4].attributes('style')).toContain('--danger')
    expect(cells[2].findAll('td')[4].text()).toBe('录制中')
  })

  it('空列表显示引导文案，不留一张空表', () => {
    const w = mount(RecordingList, { props: { recordings: [] } })
    expect(w.find('table').exists()).toBe(false)
    expect(w.text()).toContain('还没有录制记录。')
  })

  it('体积按可读单位格式化', () => {
    const w = mount(RecordingList, {
      props: { recordings: [makeRecording({ size: 1536 * 1024 })] },
    })
    expect(w.findAll('tbody tr')[0].findAll('td')[3].text()).toBe('1.5 MB')
  })
})
