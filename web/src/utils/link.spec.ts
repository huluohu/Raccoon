import { describe, expect, it } from 'vitest'
import { checkLink, extractUrl } from '@/utils/link'

/**
 * link.ts 用例（提交前的链接格式闸）。
 *
 * 关键验收点：
 * 1) 分享文案能提取出裸链接——这是最高频的真实输入形态。
 * 2) 结尾的中英文标点必须剔除（它们是文案不是链接，带进去引擎会拿到 404 路径）。
 * 3) 缺协议头要给「补上 http://」而不是「格式错误」——提示要指得清下一步。
 * 4) 校验通过时返回提取后的干净链接。
 */
describe('extractUrl', () => {
  it('纯链接原样返回', () => {
    expect(extractUrl('https://www.bilibili.com/video/BV1xx411c7mD')).toBe(
      'https://www.bilibili.com/video/BV1xx411c7mD',
    )
  })

  it('从 B 站分享文案里提取链接', () => {
    expect(extractUrl('【测试视频】https://b23.tv/abc123 复制打开bilibili')).toBe(
      'https://b23.tv/abc123',
    )
  })

  it('从抖音分享文案里提取链接（带路径斜杠）', () => {
    expect(extractUrl('3.14 复制打开抖音，看看… https://v.douyin.com/xyz/')).toBe(
      'https://v.douyin.com/xyz/',
    )
  })

  it('剔除结尾的中英文标点与括号', () => {
    expect(extractUrl('看这个 https://example.com/a，（超好）')).toBe('https://example.com/a')
    expect(extractUrl('看这个 https://example.com/b）')).toBe('https://example.com/b')
    expect(extractUrl('看这个 https://example.com/c。')).toBe('https://example.com/c')
  })

  it('没有链接时返回空串', () => {
    expect(extractUrl('就是一段文字')).toBe('')
    expect(extractUrl('www.example.com/a')).toBe('')
  })
})

describe('checkLink', () => {
  it('空输入提示先粘贴链接', () => {
    const r = checkLink('   ')
    expect(r.ok).toBe(false)
    expect(r.message).toContain('先粘贴一个链接')
  })

  it('缺协议头时明确告诉用户补 http://', () => {
    const r = checkLink('www.bilibili.com/video/BV1xx')
    expect(r.ok).toBe(false)
    expect(r.message).toContain('http://')
  })

  it('完全不像链接时给通用提示', () => {
    const r = checkLink('就是一段文字')
    expect(r.ok).toBe(false)
    expect(r.message).toContain('没找到链接')
  })

  it('合法链接通过并返回干净 URL', () => {
    const r = checkLink('https://www.bilibili.com/video/BV1xx411c7mD')
    expect(r.ok).toBe(true)
    expect(r.message).toBe('')
    expect(r.url).toBe('https://www.bilibili.com/video/BV1xx411c7mD')
  })

  it('分享文案通过校验，提交的是提取后的链接（不是整段文案）', () => {
    const r = checkLink('【标题】https://b23.tv/abc 复制打开bilibili')
    expect(r.ok).toBe(true)
    expect(r.url).toBe('https://b23.tv/abc')
  })

  it('缺主机名的链接给出明确提示', () => {
    const r = checkLink('https://')
    expect(r.ok).toBe(false)
    expect(r.message).toContain('主机名')
  })
})
