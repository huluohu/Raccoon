import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SubForm from '@/components/subs/SubForm.vue'
import { RButtonStub } from '@/test/stubs'
import type { ParsePreview } from '@/api/types'

/**
 * SubForm 用例（docs/07 §3「/subscriptions → SubForm（先预览后创建）」）。
 *
 * 关键验收点：
 * 1) 「先预览后创建」是铁律：不解析成功就不让保存。
 *    这条判定只能有一处（组件内 canSave），不能散落在父组件的按钮条件里。
 * 2) 保存按钮的文案要跟着状态走（请先解析预览 → 创建订阅 → 保存中…）。
 * 3) 「指定区间」模式下才出现起止条目输入——其他模式不占地方。
 * 4) 表单值留在组件内部，只通过 sourceUrl 把当前链接暴露给父组件（不发请求）。
 * 5) initial 变化要重置表单（父组件每次打开抽屉都重置成默认值）。
 */
const stubs = { RButton: RButtonStub }

function makePreview(over: Partial<ParsePreview> = {}): ParsePreview {
  return {
    url: 'https://space.bilibili.com/123/favlist?fid=456',
    platform: 'bilibili',
    title: '我的收藏夹',
    is_playlist: true,
    kind: 'playlist',
    formats: [{ quality: '1080p' }, { quality: '720p' }],
    ...over,
  }
}

describe('SubForm 组件', () => {
  it('没解析预览时不能保存（按钮禁用 + 文案说明原因）', () => {
    const w = mount(SubForm, { global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('请先解析预览'))!
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('解析预览成功后按钮变「创建订阅」且可提交', async () => {
    const w = mount(SubForm, { props: { preview: makePreview() }, global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('创建订阅'))!
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('disabled')).toBeUndefined()
  })

  it('点「解析预览」抛 preview 事件（由父组件去调 api）', async () => {
    const w = mount(SubForm, { global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('解析预览'))!
    await btn.trigger('click')
    expect(w.emitted('preview')).toBeTruthy()
  })

  it('解析预览结果要展示标题/平台/类型（合集显示项数）', () => {
    const w = mount(SubForm, { props: { preview: makePreview() }, global: { stubs } })
    expect(w.find('.pv-title').text()).toBe('我的收藏夹')
    expect(w.text()).toContain('bilibili')
    expect(w.text()).toContain('集合（2 项）')
  })

  it('单视频来源显示「单视频」而不是集合', () => {
    const w = mount(SubForm, {
      props: { preview: makePreview({ is_playlist: false, formats: [] }) },
      global: { stubs },
    })
    expect(w.text()).toContain('单视频')
  })

  it('提交时抛出完整表单值', async () => {
    const w = mount(SubForm, { props: { preview: makePreview() }, global: { stubs } })
    await w.find('form').trigger('submit')
    const saved = w.emitted('save')![0][0] as Record<string, unknown>
    expect(saved.mode).toBe('new_only')
    expect(saved.interval_min).toBe(30)
    expect(saved.source_url).toBe('')
  })

  it('没预览就提交不会抛 save（保存按钮禁用是唯一入口，这里再兜一层）', async () => {
    const w = mount(SubForm, { global: { stubs } })
    await w.find('form').trigger('submit')
    expect(w.emitted('save')).toBeFalsy()
  })

  it('「指定区间」模式下才出现起止条目输入', async () => {
    const w = mount(SubForm, { props: { preview: makePreview() }, global: { stubs } })
    expect(w.find('.range-row').exists()).toBe(false)

    // 第二个 select 是「抓取模式」（第一个是来源类型）
    const selects = w.findAll('select')
    await selects[1].setValue('range')
    expect(w.find('.range-row').exists()).toBe(true)
  })

  it('解析中/保存中：对应按钮禁用并改文案', () => {
    const w = mount(SubForm, { props: { previewing: true }, global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('解析中'))!
    expect(btn.attributes('disabled')).toBeDefined()

    const w2 = mount(SubForm, {
      props: { preview: makePreview(), saving: true },
      global: { stubs },
    })
    const btn2 = w2.findAll('button').find((b) => b.text().includes('保存中'))!
    expect(btn2.attributes('disabled')).toBeDefined()
  })

  it('点取消抛 cancel 事件', async () => {
    const w = mount(SubForm, { global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('取消'))!
    await btn.trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
  })

  it('暴露当前来源链接（父组件解析预览要用），不暴露整个表单对象', async () => {
    const w = mount(SubForm, { global: { stubs } })
    await w.find('textarea').setValue('https://example.com/list')
    expect((w.vm as unknown as { sourceUrl: string }).sourceUrl).toBe('https://example.com/list')
  })

  it('initial 变化时重置表单（父组件每次打开抽屉都重置）', async () => {
    const w = mount(SubForm, {
      props: { initial: { source_url: 'https://a.example.com', interval_min: 60 } },
      global: { stubs },
    })
    await w.setProps({ initial: { source_url: 'https://b.example.com' } })
    expect((w.vm as unknown as { sourceUrl: string }).sourceUrl).toBe('https://b.example.com')
    const interval = w.find('input[type="number"]').element as HTMLInputElement
    expect(interval.value).toBe('30')
  })
})
