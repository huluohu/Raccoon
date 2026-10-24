import { describe, expect, it } from 'vitest'

import PageHeader from '@/components/base/PageHeader.vue'
import { mount } from '@/test/mount'

/**
 * PageHeader 用例（docs/07 §2「页头签名」：十视图统一的指挥中心式页头）。
 */
describe('PageHeader', () => {
  it('眉题 + 句点大标题 + 副行齐备', () => {
    const w = mount(PageHeader, {
      props: { eyebrow: 'TASKS', title: '下载', sub: '粘贴链接 → 解析预览 → 加入队列' },
    })
    expect(w.find('.phead-eyebrow').text()).toContain('TASKS')
    // 句点由组件补在末尾（强调色签名），调用方只给纯标题
    expect(w.find('.phead-title').text()).toBe('下载.')
    expect(w.find('.phead-sub').text()).toContain('粘贴链接')
  })

  it('眉题带前置短横线（签名笔画的图形部分）', () => {
    const w = mount(PageHeader, { props: { eyebrow: 'OVERVIEW', title: '总览' } })
    expect(w.find('.phead-dash').exists()).toBe(true)
  })

  it('无副行时不渲染空元素占位', () => {
    const w = mount(PageHeader, { props: { eyebrow: 'MEDIA', title: '媒体库' } })
    expect(w.find('.phead-sub').exists()).toBe(false)
  })

  it('actions 插槽渲染右侧动作区；无插槽不占位', () => {
    const withActions = mount(PageHeader, {
      props: { eyebrow: 'SETTINGS', title: '设置' },
      slots: { actions: '<button class="stub-act">保存改动</button>' },
    })
    expect(withActions.find('.phead-actions').exists()).toBe(true)
    expect(withActions.find('.stub-act').text()).toBe('保存改动')

    const bare = mount(PageHeader, { props: { eyebrow: 'MEDIA', title: '媒体库' } })
    expect(bare.find('.phead-actions').exists()).toBe(false)
  })

  it('#sub 插槽承载富内容（实时状态点等），优先于 sub 文本', () => {
    const w = mount(PageHeader, {
      props: { eyebrow: 'TASKS', title: '下载', sub: '兜底文本' },
      slots: { sub: '<span class="stub-sub">实时已连接</span>' },
    })
    expect(w.find('.stub-sub').text()).toBe('实时已连接')
    expect(w.find('.phead-sub').text()).not.toContain('兜底文本')
  })
})
