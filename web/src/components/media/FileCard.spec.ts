import { describe, expect, it } from 'vitest'

import FileCard from '@/components/media/FileCard.vue'
import type { MediaFile } from '@/api/types'
import { mount } from '@/test/mount'

/**
 * FileCard 用例（docs/07 §3「/media → FileTable/FileCard」）。
 *
 * 关键验收点：
 * 1) 图片才用缩略图；视频/音频用类型化占位——不给假图。
 * 2) 缩略图 URL 由父组件注入（组件不直接依赖 api 层，便于单测）。
 * 3) 文件名 + 大小 + 时长都是真实数据，不编。
 * 4) 占位不是英文 kind 原词：SVG 类型图标 + 中文类型名，底色注入平台品牌色。
 */
const stubs = {
  PlatformBadge: { props: ['platform'], template: '<i class="stub-badge" :data-platform="platform" />' },
  RButton: { template: '<button @click="$emit(\'click\')"><slot /></button>' },
}

function makeFile(over: Partial<MediaFile> = {}): MediaFile {
  return {
    id: 'f-1',
    path: '/downloads/bilibili/a.mp4',
    name: 'a.mp4',
    size: 100 * 1024 * 1024,
    kind: 'video',
    previewable: true,
    platform: 'bilibili',
    duration: 125,
    created_at: 1_700_000_000_000,
    ...over,
  }
}

describe('FileCard 组件', () => {
  it('渲染文件名、大小、时长与平台徽章', () => {
    const w = mount(FileCard, {
      props: { file: makeFile(), previewUrl: (id) => `/api/v1/files/${id}/preview` },
      global: { stubs },
    })
    expect(w.find('.file-name').text()).toBe('a.mp4')
    expect(w.text()).toContain('100.0 MB')
    expect(w.text()).toContain('2:05')
    expect(w.find('.stub-badge').attributes('data-platform')).toBe('bilibili')
  })

  it('图片类才用真实缩略图（走注入的 URL 生成器）', () => {
    const w = mount(FileCard, {
      props: {
        file: makeFile({ id: 'f-img', kind: 'image', name: 'a.png' }),
        previewUrl: (id) => `/preview/${id}`,
      },
      global: { stubs },
    })
    const img = w.find('.file-thumb-img img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('/preview/f-img')
    // 图片要有 alt（无障碍）
    expect(img.attributes('alt')).toBe('a.png')
  })

  it('视频/音频用类型化占位，不给假图（诚实原则）', () => {
    const w = mount(FileCard, {
      props: { file: makeFile({ kind: 'video' }), previewUrl: () => '/x' },
      global: { stubs },
    })
    expect(w.find('.file-thumb-img img').exists()).toBe(false)
    // 占位不是英文原词：中文类型名 + 类型图标
    expect(w.find('.file-kind').text()).toBe('视频')
    expect(w.find('.file-kind-icon').exists()).toBe(true)
  })

  it('占位底色注入平台品牌色（卡片在网格里可辨认是谁家的）', () => {
    const w = mount(FileCard, {
      props: { file: makeFile({ platform: 'bilibili' }), previewUrl: () => '/x' },
      global: { stubs },
    })
    // platformTone('bilibili') 是 B 站品牌色，color-mix 淡化后进 linear-gradient
    expect(w.find('.file-thumb').attributes('style')).toContain('linear-gradient')
    // 图片类不走品牌色注入（有真图）
    const img = mount(FileCard, {
      props: { file: makeFile({ kind: 'image', name: 'a.png' }), previewUrl: () => '/x' },
      global: { stubs },
    })
    expect(img.find('.file-thumb').attributes('style')).toBeUndefined()
  })

  it('没有时长就不显示时长（不显示 0:00 骗人）', () => {
    const w = mount(FileCard, {
      props: { file: makeFile({ duration: undefined }), previewUrl: () => '/x' },
      global: { stubs },
    })
    expect(w.text()).not.toContain('0:00')
  })

  it('点缩略图抛 open 事件（带文件对象）', async () => {
    const file = makeFile()
    const w = mount(FileCard, {
      props: { file, previewUrl: () => '/x' },
      global: { stubs },
    })
    await w.find('.file-open').trigger('click')
    expect(w.emitted('open')![0]).toEqual([file])
  })

  it('点「下载」抛 download 事件，点「详情」抛 open 事件', async () => {
    const file = makeFile()
    const w = mount(FileCard, {
      props: { file, previewUrl: () => '/x' },
      global: { stubs },
    })
    const btns = w.findAll('button')
    const dl = btns.find((b) => b.text().includes('下载'))!
    const detail = btns.find((b) => b.text().includes('详情'))!

    await dl.trigger('click')
    expect(w.emitted('download')![0]).toEqual([file])

    await detail.trigger('click')
    expect(w.emitted('open')![0]).toEqual([file])
  })

  it('次要动作收进「更多」菜单：提音频在菜单里，主行只留三个', async () => {
    const file = makeFile({ kind: 'video', name: 'clip.webm' })
    const w = mount(FileCard, {
      props: { file, previewUrl: () => '/x' },
      global: { stubs },
    })
    // 主行只有 下载/详情/删除 + 「更多」触发器（5 个按钮横排会出界）
    const mainLabels = w.findAll('.file-actions > button, .file-actions > .rmenu').map((b: any) => b.text())
    expect(mainLabels.filter((l: string) => l.includes('提音频')).length).toBe(0)
    expect(w.find('.rmenu').exists()).toBe(true)

    // 打开菜单后能点到「提取 MP3 音频」
    await w.find('.rmenu-trigger-badge').trigger('click')
    const item = w.findAll('.rmenu-item').find((b: any) => b.text().includes('MP3'))
    expect(item).toBeTruthy()
    await item!.trigger('click')
    expect(w.emitted('convert')?.[0]).toEqual([file, 'mp3'])
  })

  it('「更多」菜单含封面制作与元数据编辑入口', async () => {
    const file = makeFile({ kind: 'video', name: 'clip.webm' })
    const w = mount(FileCard, {
      props: { file, previewUrl: () => '/x' },
      global: { stubs },
    })
    await w.find('.rmenu-trigger-badge').trigger('click')
    const labels = w.findAll('.rmenu-item').map((b: any) => b.text())
    expect(labels.some((l: string) => l.includes('制作封面'))).toBe(true)
    expect(labels.some((l: string) => l.includes('编辑元数据'))).toBe(true)
    expect(labels.some((l: string) => l.includes('刮削'))).toBe(true)
  })

  it('非 MP4 视频的「更多」菜单含转 MP4 动作', async () => {
    const file = makeFile({ kind: 'video', name: 'clip.webm' })
    const w = mount(FileCard, {
      props: { file, previewUrl: () => '/x' },
      global: { stubs },
    })
    await w.find('.rmenu-trigger-badge').trigger('click')
    const mp4Item = w.findAll('.rmenu-item').find((b: any) => b.text().includes('MP4'))
    expect(mp4Item).toBeTruthy()
    await mp4Item!.trigger('click')
    expect(w.emitted('convert')?.[0]).toEqual([file, 'mp4'])
  })

  it('有封面时用封面当缩略图，无封面时回退类型占位', async () => {
    const withCover = makeFile({ kind: 'video', name: 'clip.webm', cover_path: '/x.cover.jpg' })
    const w1 = mount(FileCard, {
      props: { file: withCover, previewUrl: () => '/p', coverUrl: (id) => `/cover/${id}` },
      global: { stubs },
    })
    expect(w1.find('.file-thumb-img img').attributes('src')).toBe('/cover/f-1')

    const noCover = makeFile({ kind: 'video', name: 'clip.webm' })
    const w2 = mount(FileCard, {
      props: { file: noCover, previewUrl: () => '/p' },
      global: { stubs },
    })
    expect(w2.find('.file-thumb-img').exists()).toBe(false)
    expect(w2.find('.file-kind').exists()).toBe(true)
  })

  it('展示标题优先用用户改过的 title，回退文件名', () => {
    const withTitle = makeFile({ kind: 'video', name: 'raw-name.mp4', title: '我改过的标题' })
    const w1 = mount(FileCard, {
      props: { file: withTitle, previewUrl: () => '/x' },
      global: { stubs },
    })
    expect(w1.find('.file-name').text()).toBe('我改过的标题')

    const noTitle = makeFile({ kind: 'video', name: 'raw-name.mp4' })
    const w2 = mount(FileCard, {
      props: { file: noTitle, previewUrl: () => '/x' },
      global: { stubs },
    })
    expect(w2.find('.file-name').text()).toBe('raw-name.mp4')
  })
})
