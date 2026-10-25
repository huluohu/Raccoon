<script setup lang="ts">
// MediaMetaDialog：媒体元数据编辑对话框。
//
// 能改的：标题 / 作者 / 简介 / 标签 / 时长。
// 也提供「AI 刮削」入口：从字幕/简介自动提取结构化元数据，刮削结果先回填表单
// 由用户确认后再保存——不直接覆盖（AI 可能抽错，用户有权改）。
import { ref, watch } from 'vue'
import { api } from '@/api/client'
import type { MediaFile } from '@/api/types'
import RDialog from '@/components/base/RDialog.vue'
import RButton from '@/components/base/RButton.vue'
import RInput from '@/components/base/RInput.vue'

const props = defineProps<{
  open: boolean
  file: MediaFile | null
}>()

const emit = defineEmits<{
  (e: 'update:open', val: boolean): void
  (e: 'done', file: MediaFile): void
}>()

const form = ref({ title: '', uploader: '', description: '', tags: '', duration: '' })
const saving = ref(false)
const scraping = ref(false)
const errMsg = ref('')

watch(
  () => props.open,
  (open) => {
    if (!open || !props.file) return
    form.value = {
      title: props.file.title || props.file.name,
      uploader: props.file.uploader || '',
      description: props.file.description || '',
      tags: props.file.tags || '',
      duration: props.file.duration ? String(props.file.duration) : '',
    }
    errMsg.value = ''
  },
)

async function scrape() {
  if (!props.file) return
  scraping.value = true
  errMsg.value = ''
  try {
    const res = await api.scrapeMedia(props.file.id)
    const r = res.result
    form.value = {
      title: r.title || form.value.title,
      uploader: r.uploader || form.value.uploader,
      description: r.description || form.value.description,
      tags: (r.tags ?? []).join(', ') || form.value.tags,
      duration: form.value.duration,
    }
  } catch (e: unknown) {
    errMsg.value = e instanceof Error ? e.message : 'AI 刮削失败'
  } finally {
    scraping.value = false
  }
}

async function save() {
  if (!props.file) return
  saving.value = true
  errMsg.value = ''
  try {
    const dur = form.value.duration.trim() === '' ? null : Number(form.value.duration.trim())
    const res = await api.updateMediaMeta(props.file.id, {
      title: form.value.title.trim(),
      uploader: form.value.uploader.trim(),
      description: form.value.description.trim(),
      tags: form.value.tags.trim(),
      duration: dur !== null && !Number.isNaN(dur) ? dur : undefined,
    })
    emit('done', res.file)
    emit('update:open', false)
  } catch (e: unknown) {
    errMsg.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <RDialog
    :open="open"
    title="编辑元数据"
    width="480px"
    @update:open="emit('update:open', $event)"
  >
    <div class="meta-form">
      <RInput v-model="form.title" label="标题" placeholder="展示标题" />
      <RInput v-model="form.uploader" label="作者 / UP主" placeholder="内容作者" />
      <RInput v-model="form.tags" label="标签" placeholder="逗号分隔，如 科普, 宇宙" />
      <RInput v-model="form.duration" label="时长（秒）" placeholder="视频时长" />
      <label class="meta-field">
        <span class="meta-label">简介</span>
        <textarea
          v-model="form.description"
          class="meta-textarea"
          rows="4"
          placeholder="内容简介 / 描述"
        />
      </label>

      <p v-if="errMsg" class="meta-err" role="alert">{{ errMsg }}</p>

      <div class="meta-foot">
        <RButton size="sm" variant="ghost" :disabled="scraping" @click="scrape">
          {{ scraping ? '刮削中…' : 'AI 刮削' }}
        </RButton>
        <span class="meta-foot-spacer" />
        <RButton size="sm" variant="ghost" @click="emit('update:open', false)">取消</RButton>
        <RButton size="sm" variant="accent" :disabled="saving" @click="save">
          {{ saving ? '保存中…' : '保存' }}
        </RButton>
      </div>
    </div>
  </RDialog>
</template>

<style scoped>
.meta-form { display: flex; flex-direction: column; gap: 12px; }
.meta-field { display: flex; flex-direction: column; gap: 6px; }
.meta-label {
  font-size: var(--font-aux);
  font-weight: var(--weight-caption);
  color: var(--text-2);
}
.meta-textarea {
  width: 100%;
  box-sizing: border-box;
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-ctrl);
  background: var(--surface);
  color: var(--text-1);
  font-size: var(--font-body);
  font-family: inherit;
  resize: vertical;
}
.meta-textarea:focus { outline: 2px solid var(--gold); outline-offset: 1px; }
.meta-err {
  font-size: var(--font-aux);
  color: var(--danger);
  margin: 0;
}
.meta-foot {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-top: 4px;
}
.meta-foot-spacer { flex: 1; }
</style>
