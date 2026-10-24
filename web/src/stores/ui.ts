import { defineStore } from 'pinia'
import { ref } from 'vue'

// UI 偏好（immediate 类配置）：主题即时切换并持久化（docs/07 §2 四套浣熊主题）。
//
// 主题 = 换皮：每个 id 对应 tokens.css 的一个完整 token 组 + 一只浣熊形象
//（--logo-* 变量），不是「深浅开关」。

export type ThemeId = 'night' | 'scarf' | 'snow' | 'berry'

/** 主题元数据：切换器渲染用（label 给中文主标签，en 给等宽副标签，swatch 是强调色样本）。 */
export const THEMES: { id: ThemeId; label: string; en: string; swatch: string }[] = [
  { id: 'night', label: '夜行', en: 'NIGHT', swatch: '#35E0A6' },
  { id: 'scarf', label: '围巾', en: 'SCARF', swatch: '#F5B93C' },
  { id: 'snow', label: '雪绒', en: 'SNOW', swatch: '#7FD8CC' },
  { id: 'berry', label: '浆果', en: 'BERRY', swatch: '#FF7A85' },
]

const STORAGE_KEY = 'raccoon.theme'

/** 读取并迁移 localStorage：旧版 light/dark 二值映射到新主题（dark→night，light→snow）。 */
function loadTheme(): ThemeId {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === 'dark') return 'night'
    if (raw === 'light') return 'snow'
    if (raw === 'night' || raw === 'scarf' || raw === 'snow' || raw === 'berry') return raw
  } catch {
    // 隐私模式等 localStorage 不可用场景：用默认主题，切换照常（仅不持久化）。
  }
  return 'night'
}

export const useUiStore = defineStore('ui', () => {
  const theme = ref<ThemeId>(loadTheme())
  const density = ref<'standard' | 'compact'>(
    (localStorage.getItem('raccoon.density') as 'standard' | 'compact') || 'standard',
  )

  function setTheme(id: ThemeId) {
    theme.value = id
    try {
      localStorage.setItem(STORAGE_KEY, id)
    } catch {
      // 不持久化不阻断切换。
    }
  }

  function toggleDensity() {
    density.value = density.value === 'compact' ? 'standard' : 'compact'
    localStorage.setItem('raccoon.density', density.value)
  }

  return { theme, density, setTheme, toggleDensity, themes: THEMES }
})
