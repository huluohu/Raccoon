import { defineStore } from 'pinia'
import { ref } from 'vue'

// UI 偏好（immediate 类配置）：深浅色即时切换并持久化（方案 §10.8）。
export const useUiStore = defineStore('ui', () => {
  const theme = ref<'light' | 'dark'>((localStorage.getItem('raccoon.theme') as 'light' | 'dark') || 'light')
  const density = ref<'standard' | 'compact'>((localStorage.getItem('raccoon.density') as 'standard' | 'compact') || 'standard')

  function toggleTheme() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
    localStorage.setItem('raccoon.theme', theme.value)
  }

  function toggleDensity() {
    density.value = density.value === 'compact' ? 'standard' : 'compact'
    localStorage.setItem('raccoon.density', density.value)
  }

  return { theme, density, toggleTheme, toggleDensity }
})
