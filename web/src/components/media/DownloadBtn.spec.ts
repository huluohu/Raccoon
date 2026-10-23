import { describe, expect, it, vi, afterEach } from 'vitest'

import DownloadBtn from '@/components/media/DownloadBtn.vue'
import { mount } from '@/test/mount'

/**
 * DownloadBtn 用例（docs/07 §3「/media → DownloadBtn」）。
 *
 * 关键验收点：
 * 1) 下载必须是 <a href download>（浏览器原生下载 + 右键另存为 + 读屏念成链接），
 *    不是 <button>。
 * 2) 点完给「下载中…」反馈（1.5s），防止用户以为没点上而反复点。
 * 3) 下载中期间禁用，不重复触发。
 */
afterEach(() => {
  vi.useRealTimers()
})

describe('DownloadBtn 组件', () => {
  it('渲染成带 download 属性的 <a>（语义正确）', () => {
    const w = mount(DownloadBtn, { props: { href: '/api/v1/files/f-1/download' } })
    const a = w.find('a')
    expect(a.exists()).toBe(true)
    expect(a.attributes('href')).toBe('/api/v1/files/f-1/download')
    expect(a.attributes('download')).toBeDefined()
  })

  it('有可读的无障碍名称', () => {
    const w = mount(DownloadBtn, { props: { href: '/x', label: '下载视频' } })
    expect(w.find('a').attributes('aria-label')).toBe('下载 下载视频')
  })

  it('点击后显示「下载中…」并禁用', async () => {
    vi.useFakeTimers()
    const w = mount(DownloadBtn, { props: { href: '/x' } })
    expect(w.text()).toContain('下载')

    await w.find('a').trigger('click')
    expect(w.text()).toContain('下载中')
    expect(w.find('a').classes()).toContain('rbtn--disabled')
  })

  it('1.5s 后自动恢复（不永久卡在下载中）', async () => {
    vi.useFakeTimers()
    const w = mount(DownloadBtn, { props: { href: '/x' } })
    await w.find('a').trigger('click')
    expect(w.text()).toContain('下载中')

    vi.advanceTimersByTime(1500)
    await w.vm.$nextTick()
    expect(w.text()).not.toContain('下载中')
  })

  it('重复点击会重置计时（不是叠两个定时器）', async () => {
    vi.useFakeTimers()
    const w = mount(DownloadBtn, { props: { href: '/x' } })
    await w.find('a').trigger('click')
    vi.advanceTimersByTime(1000)
    await w.find('a').trigger('click')
    // 再等 1.5s：从第二次点击算起已过 1.5s，应已恢复
    vi.advanceTimersByTime(1500)
    await w.vm.$nextTick()
    expect(w.text()).not.toContain('下载中')
  })
})
