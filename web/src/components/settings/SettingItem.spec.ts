import { describe, expect, it } from 'vitest'

import SettingItem from '@/components/settings/SettingItem.vue'
import type { ConfigItem } from '@/api/types'
import { mount } from '@/test/mount'

/**
 * SettingItem 用例（docs/07 §3「SettingItem（生效时机徽章 + 脏圆点 + 保存反馈）」）。
 *
 * 关键验收点：
 * 1) 生效时机徽章来自后端 `effective`，前端不猜（铁律）。
 * 2) 敏感项不回填掩码：后端只回 `******`，回填后保存会把掩码写进配置。
 * 3) env 锁定项只读，并如实标出是哪个环境变量锁的。
 * 4) 保存反馈按后端 effective_summary 的说法展示，不自己编时机。
 */
function makeItem(over: Partial<ConfigItem> = {}): ConfigItem {
  return {
    key: 'download.max_concurrent',
    desc: '同时下载的任务数上限',
    default: 3,
    value: 3,
    type: 'int',
    effective: 'next_task',
    sensitive: false,
    locked: false,
    version: 2,
    ...over,
  }
}

describe('SettingItem 组件', () => {
  it('渲染配置键名、说明与默认值', () => {
    const w = mount(SettingItem, {
      props: { item: makeItem(), modelValue: 3, dirty: false },
    })
    expect(w.find('.setting-key').text()).toBe('download.max_concurrent')
    expect(w.find('.setting-desc').text()).toBe('同时下载的任务数上限')
    expect(w.find('.setting-default').text()).toContain('默认 3')
  })

  it('生效时机徽章按后端 effective 显示（前端不猜）', () => {
    const cases: [ConfigItem['effective'], string][] = [
      ['immediate', '立即生效'],
      ['next_task', '下个任务生效'],
      ['restart', '需重启生效'],
    ]
    for (const [effective, text] of cases) {
      const w = mount(SettingItem, {
        props: { item: makeItem({ effective }), modelValue: 3, dirty: false },
      })
      const badge = w.find('.eff-badge')
      expect(badge.text()).toBe(text)
      expect(badge.classes()).toContain(`eff-badge--${effective}`)
    }
  })

  it('脏状态显示脏圆点，清掉后消失', async () => {
    const w = mount(SettingItem, {
      props: { item: makeItem(), modelValue: 6, dirty: true },
    })
    expect(w.find('.dirty-pip').exists()).toBe(true)

    await w.setProps({ dirty: false })
    expect(w.find('.dirty-pip').exists()).toBe(false)
  })

  it('保存反馈显示在项上（按后端下发的生效时机）', () => {
    const w = mount(SettingItem, {
      props: {
        item: makeItem(),
        modelValue: 6,
        dirty: false,
        savedText: '已保存 · 下个任务生效',
      },
    })
    expect(w.find('.saved-badge').text()).toBe('已保存 · 下个任务生效')
  })

  it('没有保存反馈时不留空位', () => {
    const w = mount(SettingItem, {
      props: { item: makeItem(), modelValue: 3, dirty: false },
    })
    expect(w.find('.saved-badge').exists()).toBe(false)
  })

  it('敏感项不回填掩码：输入框为空，只在旁注展示当前掩码', () => {
    const w = mount(SettingItem, {
      props: {
        item: makeItem({ key: 'qb.password', type: 'string', sensitive: true, value: '******' }),
        modelValue: '',
        dirty: false,
      },
    })
    // 关键：绝不能把掩码当值回填进输入框（否则保存会把掩码写进配置）
    expect((w.find('input[type="password"]').element as HTMLInputElement).value).toBe('')
    expect(w.find('.sens-badge').text()).toBe('敏感')
    expect(w.find('.sens-state').text()).toBe('当前：******')
  })

  it('敏感项未设置时显示「未设置」而不是空白', () => {
    const w = mount(SettingItem, {
      props: {
        item: makeItem({ key: 'qb.password', type: 'string', sensitive: true, value: '' }),
        modelValue: '',
        dirty: false,
      },
    })
    expect(w.find('.sens-state').text()).toBe('当前：未设置')
  })

  it('env 锁定的项只读，并标出是哪个环境变量锁的', () => {
    const w = mount(SettingItem, {
      props: {
        item: makeItem({ key: 'server.port', locked: true, env_var: 'RACCOON_PORT', value: 8080 }),
        modelValue: 8080,
        dirty: false,
      },
    })
    const lock = w.find('.lock-badge')
    expect(lock.text()).toContain('RACCOON_PORT')
    expect(lock.attributes('title')).toContain('RACCOON_PORT')
    const input = w.find('input')
    expect(input.attributes('disabled')).toBeDefined()
    expect((input.element as HTMLInputElement).value).toBe('8080')
  })

  it('锁定的敏感项也只回显掩码，不显示明文', () => {
    const w = mount(SettingItem, {
      props: {
        item: makeItem({
          key: 'n115.password',
          type: 'string',
          sensitive: true,
          locked: true,
          env_var: 'RACCOON_N115_PASSWORD',
          value: '******',
        }),
        modelValue: '',
        dirty: false,
      },
    })
    expect((w.find('input[type="password"]').element as HTMLInputElement).value).toBe('')
    expect(w.find('.sens-state').text()).toBe('当前：******')
  })

  it('bool 项渲染成开关，开=真 关=假', () => {
    const w = mount(SettingItem, {
      props: { item: makeItem({ key: 'server.debug', type: 'bool', value: false }), modelValue: false, dirty: false },
    })
    const box = w.find('input[type="checkbox"]')
    expect((box.element as HTMLInputElement).checked).toBe(false)
    expect(w.find('.switch span').text()).toBe('关')
  })

  it('int 项输入回传 number；清空回落到原值（不等于设成 0）', async () => {
    const w = mount(SettingItem, {
      props: { item: makeItem(), modelValue: 3, dirty: false },
    })
    const input = w.find('input[type="number"]')

    await input.setValue('6')
    // input 与 change 都会触发（与原视图行为一致），取最后一次为准
    const emitted = w.emitted('update:modelValue')!
    expect(emitted[emitted.length - 1]).toEqual([6])

    await input.setValue('')
    const emitted2 = w.emitted('update:modelValue')!
    expect(emitted2[emitted2.length - 1]).toEqual([3])
  })

  it('bool 项切换回传布尔值', async () => {
    const w = mount(SettingItem, {
      props: { item: makeItem({ key: 'server.debug', type: 'bool' }), modelValue: false, dirty: false },
    })
    await w.find('input[type="checkbox"]').setValue(true)
    expect(w.emitted('update:modelValue')![0]).toEqual([true])
  })

  it('枚举项用后端 choices 渲染下拉（前端不写死值域）', () => {
    const w = mount(SettingItem, {
      props: {
        item: makeItem({ key: 'download.format', type: 'string', choices: ['mp4', 'mkv', 'webm'] }),
        modelValue: 'mp4',
        dirty: false,
      },
    })
    const opts = w.findAll('option').map((o) => o.attributes('value'))
    expect(opts).toEqual(['mp4', 'mkv', 'webm'])
  })

  it('没有 choices 的项渲染自由文本输入框', () => {
    const w = mount(SettingItem, {
      props: { item: makeItem({ key: 'storage.save_path', type: 'string' }), modelValue: '/media', dirty: false },
    })
    expect(w.find('input[type="text"]').exists()).toBe(true)
  })

  it('net.proxy 项渲染「测试代理连接」按钮', () => {
    const w = mount(SettingItem, {
      props: {
        item: makeItem({
          key: 'net.proxy',
          desc: '网络代理',
          type: 'string',
          default: '',
          value: 'http://127.0.0.1:7890',
        }),
        modelValue: 'http://127.0.0.1:7890',
        dirty: false,
      },
    })
    expect(w.find('.proxy-test-box').exists()).toBe(true)
    expect(w.find('.proxy-test-box button').text()).toContain('测试代理连接')
  })
})
