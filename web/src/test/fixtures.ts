import type { Task, TaskEvent, TaskFile } from '@/api/types'

/** 测试用任务夹具：字段与后端 store.Task 对齐，值全部虚构。 */
export function makeTask(over: Partial<Task> = {}): Task {
  return {
    id: 't-1',
    kind: 'download',
    source: 'web',
    platform: 'bilibili',
    url: 'https://www.bilibili.com/video/BV1xx411c7mD',
    title: '测试视频 · 第 1 集',
    status: 'downloading',
    quality: '1080p',
    expected_size: 100 * 1024 * 1024,
    downloaded_size: 40 * 1024 * 1024,
    speed_bps: 2 * 1024 * 1024,
    progress: 40,
    resume_supported: true,
    engine: 'yt-dlp',
    engine_version: '2025.01.15',
    config_version: 1,
    attempt: 0,
    max_attempts: 3,
    cancel_requested: false,
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
    ...over,
  }
}

export function makeEvent(over: Partial<TaskEvent> = {}): TaskEvent {
  return {
    id: 1,
    task_id: 't-1',
    ts: 1_700_000_000_000,
    level: 'info',
    event: 'downloading',
    message: '开始下载',
    ...over,
  }
}

export function makeFile(over: Partial<TaskFile> = {}): TaskFile {
  return {
    id: 'f-1',
    task_id: 't-1',
    path: '/downloads/bilibili/测试视频.mp4',
    size: 100 * 1024 * 1024,
    kind: 'video',
    created_at: 1_700_000_000_000,
    ...over,
  }
}
