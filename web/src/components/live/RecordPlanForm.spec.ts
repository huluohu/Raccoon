import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import RecordPlanForm from '@/components/live/RecordPlanForm.vue'
import { RButtonStub } from '@/test/stubs'

/**
 * RecordPlanForm 用例（docs/07 §3「/live → RecordPlanForm」）。
 *
 * 关键验收点：
 * 1) 支持清单由后端下发（前端不写死）：不在清单里的平台在下拉里禁用，
 *    并且必须说明原因——「禁用了但没说为什么」是这一页的铁律破口。
 * 2) 当前选中的平台不在清单里时，提交按钮禁用 + 显示明确警告。
 * 3) 直播间地址为空不能提交（否则后端只会回 422）。
 * 4) 录制计划要带说明文案（用户知道自己选了什么）。
 */
const stubs = { RButton: RButtonStub }

describe('RecordPlanForm 组件', () => {
  it('清单外的平台在下拉里禁用并标出原因', () => {
    const w = mount(RecordPlanForm, {
      props: { supported: ['bilibili'] },
      global: { stubs },
    })
    const options = w.findAll('option')
    const douyu = options.find((o) => o.text().includes('斗鱼'))!
    expect(douyu.attributes('disabled')).toBeDefined()
    expect(douyu.text()).toContain('未实测，不支持')

    const bili = options.find((o) => o.text().includes('哔哩哔哩'))!
    expect(bili.attributes('disabled')).toBeUndefined()
  })

  it('清单说明由后端给的话术原样展示', () => {
    const w = mount(RecordPlanForm, {
      props: { supported: ['bilibili'], supportedNote: '实测只跑通了 B 站' },
      global: { stubs },
    })
    expect(w.text()).toContain('实测只跑通了 B 站')
  })

  it('地址为空时不能提交（按钮禁用）', () => {
    const w = mount(RecordPlanForm, { props: { supported: ['bilibili'] }, global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('添加'))!
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('填了地址后可以提交，抛出的值完整', async () => {
    const w = mount(RecordPlanForm, { props: { supported: ['bilibili'] }, global: { stubs } })
    await w.find('input[type="text"]').setValue('https://live.bilibili.com/12345')
    const btn = w.findAll('button').find((b) => b.text().includes('添加'))!
    expect(btn.attributes('disabled')).toBeUndefined()

    await w.find('form').trigger('submit')
    const saved = w.emitted('save')![0][0] as Record<string, unknown>
    expect(saved.room_url).toBe('https://live.bilibili.com/12345')
    expect(saved.platform).toBe('bilibili')
    expect(saved.record_plan).toBe('manual')
    expect(saved.segment_min).toBe(30)
    expect(saved.check_interval_sec).toBe(120)
  })

  it('当前平台不在清单里：显示警告并禁用提交', async () => {
    const w = mount(RecordPlanForm, {
      props: { supported: ['bilibili'], initial: { platform: 'douyu' } },
      global: { stubs },
    })
    await w.find('input[type="text"]').setValue('https://live.douyu.com/1')
    expect(w.find('.field-warn').exists()).toBe(true)
    expect(w.find('.field-warn').text()).toContain('不在实测支持清单里')

    const btn = w.findAll('button').find((b) => b.text().includes('添加'))!
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('后端清单为空时不误报警告（还没拿到清单 ≠ 选错了平台）', async () => {
    const w = mount(RecordPlanForm, { props: { supported: [] }, global: { stubs } })
    await w.find('input[type="text"]').setValue('https://live.bilibili.com/1')
    expect(w.find('.field-warn').exists()).toBe(false)
  })

  it('保存中：按钮禁用并改文案', () => {
    const w = mount(RecordPlanForm, {
      props: { supported: ['bilibili'], saving: true },
      global: { stubs },
    })
    const btn = w.findAll('button').find((b) => b.text().includes('保存中'))!
    expect(btn.attributes('disabled')).toBeDefined()
  })

  it('录制计划带说明文案（用户知道自己选了什么）', async () => {
    const w = mount(RecordPlanForm, { props: { supported: ['bilibili'] }, global: { stubs } })
    expect(w.text()).toContain('只在点「开始录制」时录')
    const selects = w.findAll('select')
    await selects[1].setValue('always')
    expect(w.text()).toContain('检测到开播就自动录制')
  })

  it('点取消抛 cancel 事件', async () => {
    const w = mount(RecordPlanForm, { props: { supported: ['bilibili'] }, global: { stubs } })
    const btn = w.findAll('button').find((b) => b.text().includes('取消'))!
    await btn.trigger('click')
    expect(w.emitted('cancel')).toBeTruthy()
  })
})
