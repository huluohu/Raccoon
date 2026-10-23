import { describe, expect, it } from 'vitest'

import PreviewDrawer from '@/components/media/PreviewDrawer.vue'
import type { MediaFile } from '@/api/types'
import { mount } from '@/test/mount'

/**
 * PreviewDrawer 用例（docs/07 §3「/media → PreviewDrawer」）。
 *
 * 关键验收点（诚实原则，最重要的一条）：
 * 浏览器播不了的编码必须直说「播不了」，不能假装能播——
 * 给一个转圈不动的播放器，比说清楚更让人困惑。
 */
const stubs = {
  DetailDrawer: {
    props: ['open', 'title'],
    emits: ['close'],
    template:
      '<div class="stub-drawer" v-if="open"><p class="stub-drawer-title">{{ title }}</p>' +
      '<button class="stub-close" @click="$emit(\'close\')">关</button><slot /></div>',
  },
  RButton: {
    props: ['href', 'block', 'variant'],
    template: '<a v-if="href" :href="href" download><slot /></a><button v-else><slot /></button>',
  },
}

function makeFile(over: Partial<MediaFile> = {}): MediaFile {
  return {
    id: 'f-1',
    path: '/downloads/a.mp4',
    name: 'a.mp4',
    size: 100 * 1024 * 1024,
    kind: 'video',
    previewable: true,
    platform: 'bilibili',
    created_at: 1_700_000_000_000,
    ...over,
  }
}

function mountDrawer(file: MediaFile | null) {
  return mount(PreviewDrawer, {
    props: {
      file,
      previewUrl: (id) => `/api/v1/files/${id}/preview`,
      downloadUrl: (id) => `/api/v1/files/${id}/download`,
    },
    global: { stubs },
  })
}

describe('PreviewDrawer 组件', () => {
  it('file 为 null 时抽屉关闭', () => {
    const w = mountDrawer(null)
    expect(w.find('.stub-drawer').exists()).toBe(false)
  })

  it('打开时展示文件名、大小、路径等元信息', () => {
    const w = mountDrawer(makeFile())
    expect(w.find('.stub-drawer').exists()).toBe(true)
    expect(w.text()).toContain('a.mp4')
    expect(w.text()).toContain('100.0 MB')
    expect(w.text()).toContain('/downloads/a.mp4')
  })

  it('可预览的视频渲染播放器（走注入的预览 URL）', () => {
    const w = mountDrawer(makeFile({ id: 'f-v' }))
    const video = w.find('video')
    expect(video.exists()).toBe(true)
    expect(video.attributes('src')).toBe('/api/v1/files/f-v/preview')
  })

  it('可预览的图片渲染 img，音频渲染 audio', () => {
    const img = mountDrawer(makeFile({ kind: 'image' }))
    expect(img.find('img').exists()).toBe(true)

    const audio = mountDrawer(makeFile({ kind: 'audio' }))
    expect(audio.find('audio').exists()).toBe(true)
  })

  it('不可预览的文件直说「浏览器无法直接播放」，不假装能播（诚实原则）', () => {
    const w = mountDrawer(makeFile({ kind: 'video', previewable: false }))
    expect(w.find('video').exists()).toBe(false)
    expect(w.find('.detail-unsupported').text()).toContain('浏览器无法直接播放')
    // 同时给出下一步（下载）
    expect(w.text()).toContain('下载文件')
  })

  it('下载按钮带真实下载地址', () => {
    const w = mountDrawer(makeFile({ id: 'f-d' }))
    // RButton 用 href 模式渲染 <a download>
    const a = w.find('a')
    expect(a.exists()).toBe(true)
    expect(a.attributes('href')).toBe('/api/v1/files/f-d/download')
  })

  it('关闭时抛 close 事件', async () => {
    const w = mountDrawer(makeFile())
    await w.find('.stub-close').trigger('click')
    expect(w.emitted('close')).toBeTruthy()
  })
})
