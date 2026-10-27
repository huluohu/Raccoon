<script setup lang="ts">
// CoverPickerDialog：封面制作对话框（从视频抽帧，让用户挑一张当封面）。
//
// 为什么需要：平台给的固定封面不一定好看也不一定贴合内容；用户常想要
// 「视频里那一帧」。ffmpeg 抽 N 帧 → 用户挑 → 存为该媒体的正式封面。
import { ref, watch } from 'vue'
import { api } from '@/api/client'
import type { MediaFile } from '@/api/types'
import RDialog from '@/components/base/RDialog.vue'
import RButton from '@/components/base/RButton.vue'

const props = defineProps<{
  open: boolean
  file: MediaFile | null
}>()

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void
  (e: 'done', file: MediaFile): void
}>()

interface Candidate {
  id: string
  url: string
  at_sec: number
  label: string
}

const loading = ref(false)
const saving = ref(false)
const errMsg = ref('')
const candidates = ref<Candidate[]>([])
const picked = ref('')
/**
 * 请求代号：防止「先打开 A、马上切到 B」时 A 的迟到响应覆盖 B 的候选。
 *
 * 触发路径：点 A 的「制作封面」→ 请求 A 在飞 → 用户关掉弹窗 → 点 B 的
 * 「制作封面」→ 请求 B 在飞 → **A 的响应先回** → 候选变成 A 的画面，
 * 而标题写的是 B 的文件 → 用户点「设为封面」就把 A 的帧写到 B 身上。
 */
let generation = 0

watch(
  () => props.open,
  async (open) => {
    // 每次开关都推进代号：旧的 in-flight 响应回来时会被判定为过期。
    const gen = ++generation
    if (!open) {
      candidates.value = []
      picked.value = ''
      errMsg.value = ''
      loading.value = false
      return
    }
    if (!props.file) return
    loading.value = true
    errMsg.value = ''
    try {
      const res = await api.coverCandidates(props.file.id)
      // 过期响应直接丢弃：不碰状态也不关 loading（可能还有更新的请求在飞）。
      if (gen !== generation) return
      candidates.value = res.items ?? []
      if (candidates.value.length) picked.value = candidates.value[0].id
    } catch (e: unknown) {
      if (gen !== generation) return
      errMsg.value = e instanceof Error ? e.message : '抽取封面帧失败'
    } finally {
      if (gen === generation) loading.value = false
    }
  },
)

async function apply() {
  if (!props.file || !picked.value) return
  saving.value = true
  errMsg.value = ''
  try {
    await api.setCover(props.file.id, picked.value)
    emit('done', { ...props.file, cover_path: 'set' })
    emit('update:open', false)
  } catch (e: unknown) {
    errMsg.value = e instanceof Error ? e.message : '保存封面失败'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <RDialog
    :open="open"
    title="制作封面"
    width="560px"
    @update:open="emit('update:open', $event)"
  >
    <div class="cover-picker">
      <p class="cover-picker-hint">
        从视频里抽取若干画面，挑一张作为封面。选定后立即生效。
      </p>

      <p v-if="loading" class="cover-picker-state">正在抽取画面…</p>
      <p v-else-if="errMsg" class="cover-picker-state is-error">{{ errMsg }}</p>
      <p v-else-if="!candidates.length" class="cover-picker-state">没有可用的候选画面</p>

      <ul v-else class="cover-grid">
        <li v-for="c in candidates" :key="c.id">
          <button
            type="button"
            class="cover-cell"
            :class="{ 'is-picked': picked === c.id }"
            :aria-pressed="picked === c.id"
            @click="picked = c.id"
          >
            <img :src="c.url" :alt="`画面 ${c.label}`" loading="lazy" />
            <span class="cover-cell-label-badge tnum">{{ c.label }}</span>
          </button>
        </li>
      </ul>

      <div class="cover-picker-foot">
        <RButton size="sm" variant="ghost" @click="emit('update:open', false)">取消</RButton>
        <RButton
          size="sm"
          variant="accent"
          :disabled="!picked || saving"
          @click="apply"
        >
          {{ saving ? '保存中…' : '设为封面' }}
        </RButton>
      </div>
    </div>
  </RDialog>
</template>

<style scoped>
.cover-picker { display: flex; flex-direction: column; gap: 12px; }
.cover-picker-hint {
  font-size: var(--font-aux);
  color: var(--text-2);
  line-height: 1.6;
  margin: 0;
}
.cover-picker-state {
  font-size: var(--font-aux);
  color: var(--text-3);
  margin: 0;
  padding: 12px 0;
  text-align: center;
}
.cover-picker-state.is-error { color: var(--danger); }
.cover-grid {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  /* auto-fit（不是 auto-fill）：候选少时轨道不塌陷，右侧不留空白。
     最小列宽 240px：封面得看得清，不是缩略图级别。 */
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 10px;
  max-height: 46vh;
  overflow-y: auto;
}
.cover-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 0;
  border: 2px solid transparent;
  border-radius: var(--radius-ctrl);
  background: var(--surface-2);
  cursor: pointer;
  overflow: hidden;
  width: 100%;
}
.cover-cell:hover { border-color: var(--border); }
.cover-cell.is-picked { border-color: var(--accent); }
.cover-cell img {
  width: 100%;
  aspect-ratio: 16 / 9;
  object-fit: cover;
  display: block;
}
.cover-cell-label-badge {
  font-size: var(--font-caption);
  color: var(--text-3);
  padding: 3px 6px;
  text-align: left;
}
.cover-picker-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding-top: 4px;
}
</style>
