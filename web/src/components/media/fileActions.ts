// fileActions.ts：媒体库「次要动作」的唯一定义处。
//
// 为什么抽出来：卡片（FileCard）与表格（Media.vue 的 RTable 行操作）都要提供
// 同一组次要动作（提音频 / 转 MP4 / 制作封面 / 编辑元数据 / AI 刮削）。
// 之前两边各写一份文案与判定，结果是「卡片能做、表格不能」或文案漂移——
// 表格形态缺了这几个入口，等于同一份功能在两种视图里不等价。
//
// 这里只描述「有哪些动作、能不能做」，具体行为由调用方按 key 绑定。
import type { MediaFile } from '@/api/types'

/** 次要动作的键。 */
export type FileActionKey = 'cover' | 'mp3' | 'mp4' | 'meta' | 'scrape'

export interface FileAction {
  /** 动作键（调用方据此绑定行为）。 */
  key: FileActionKey
  /** 菜单项文案。 */
  label: string
  /** 副标题（次要说明）。 */
  hint?: string
}

/**
 * 某个文件可用的次要动作（按展示顺序）。
 *
 * 判定口径：
 * - 制作封面只给视频：ffmpeg 抽帧要画面，音频必然一帧都抽不出来
 *   （后端也只接受 video，这里保持一致，别让用户点了才报错）；
 * - 提音频 / 转 MP4 只给视频；
 * - 已经是 .mp4 的不再给「转为 MP4」（转了也没变化）。
 */
export function fileActions(f: MediaFile): FileAction[] {
  const list: FileAction[] = []
  if (f.kind === 'video') {
    list.push({ key: 'cover', label: '制作封面', hint: '从视频抽帧，挑一张当封面' })
    list.push({ key: 'mp3', label: '提取 MP3 音频' })
    if (!f.name.endsWith('.mp4')) {
      list.push({ key: 'mp4', label: '转为 MP4 格式' })
    }
  }
  list.push({ key: 'meta', label: '编辑元数据', hint: '标题 / 作者 / 简介 / 标签' })
  list.push({ key: 'scrape', label: 'AI 刮削元数据', hint: '从字幕自动提取标题与标签' })
  return list
}
