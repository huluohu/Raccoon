<script setup lang="ts">
// DiagPanel：诊断入口（方案 §3「诊断入口在设置页」）。
//
// 为什么单独成组件：
// 诊断是「出问题之后拿证据」的功能，它的交互与其它设置项完全不同——
// 其它设置项是改值，这儿是只读地看一眼 + 导出一份材料给维护者。
// 混在设置项流里容易被当成普通配置项改掉。
//
// 铁律（与后端一致）：诊断包里的敏感字段已被后端脱敏（cookie/token/password 等 → ***），
// 前端不额外做「假脱敏」，也不假装脱敏了后端没脱敏的字段。
import { computed, ref } from 'vue'
import { api } from '@/api/client'
import RButton from '@/components/base/RButton.vue'
import REmpty from '@/components/base/REmpty.vue'

const emit = defineEmits<{
  /** 导出诊断包失败（如需重新登录）。 */
  error: [message: string]
}>()

/** 预览结果（后端 ?preview=1 的返回）。 */
const preview = ref<{
  manifest: Record<string, unknown>
  log_sample: string[]
  redaction?: string
} | null>(null)

const loading = ref(false)
const exporting = ref(false)
/** 预览失败原因（如实展示，不吞掉）。 */
const loadError = ref('')

/** manifest 里的关键字段（后端结构固定，但取值可能缺省——缺就显示「—」，不编）。 */
const manifestRows = computed<{ label: string; value: string }[]>(() => {
  const m = preview.value?.manifest
  if (!m) return []
  const rows: { label: string; value: string }[] = []
  const version = m.version
  if (version !== undefined) rows.push({ label: '版本', value: String(version) })
  const generated = m.generated_at
  if (typeof generated === 'number') {
    rows.push({ label: '生成时间', value: new Date(generated).toLocaleString() })
  }
  const logLines = m.log_lines
  if (typeof logLines === 'number') rows.push({ label: '日志行数', value: String(logLines) })
  const counts = m.task_counts
  if (counts && typeof counts === 'object') {
    const entries = Object.entries(counts as Record<string, number>)
    if (entries.length) {
      rows.push({
        label: '任务统计',
        value: entries.map(([k, v]) => `${k} ${v}`).join(' · '),
      })
    }
  }
  const cfg = m.config
  if (cfg && typeof cfg === 'object') {
    rows.push({ label: '配置项', value: `${Object.keys(cfg as object).length} 项` })
  }
  return rows
})

async function loadPreview() {
  loading.value = true
  loadError.value = ''
  try {
    const res = await api.diagPreview()
    preview.value = {
      manifest: res.manifest ?? {},
      log_sample: res.log_sample ?? [],
      redaction: (res as { redaction?: string }).redaction,
    }
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : '诊断信息读取失败'
  } finally {
    loading.value = false
  }
}

/**
 * 导出诊断包（zip）。
 *
 * 走后端返回的字节流直接下载，不在前端拼 zip：
 * 前端拼的话脱敏规则就要复制一份，后端一改规则前端就跟着失配。
 *
 * 两条路径：
 * ① 有 Blob URL 能力 → 取字节流在内存里生成下载，失败能捕获并提示。
 * ② 没有 → 直接走锚点下载（后端已设 Content-Disposition: attachment），
 *    此时失败会变成浏览器导航，捕获不到，只能放弃报错提示。
 */
async function exportDiag() {
  exporting.value = true
  try {
    if (typeof URL.createObjectURL !== 'function') {
      // 降级：直接让浏览器去下载，放弃错误捕获。
      const a = document.createElement('a')
      a.href = '/api/v1/system/diag/export'
      a.download = 'raccoon-diag.zip'
      document.body.appendChild(a)
      a.click()
      a.remove()
      return
    }
    const blob = await api.diagExport()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'raccoon-diag.zip'
    document.body.appendChild(a)
    a.click()
    a.remove()
    // 立刻 revoke 会让部分浏览器来不及发起下载，延后释放。
    setTimeout(() => URL.revokeObjectURL(url), 10000)
  } catch (e) {
    emit('error', e instanceof Error ? e.message : '诊断包导出失败')
  } finally {
    exporting.value = false
  }
}
</script>

<template>
  <div class="diag">
    <p class="diag-desc">
      出问题时导一份诊断包给维护者：含脱敏日志、配置快照与任务统计。
      敏感字段（cookie / token / password 等）由服务端强制替换为 ***。
    </p>
    <div class="diag-actions">
      <RButton size="sm" :disabled="loading" @click="loadPreview">
        {{ loading ? '读取中…' : '查看诊断信息' }}
      </RButton>
      <RButton size="sm" :disabled="exporting" @click="exportDiag">
        {{ exporting ? '导出中…' : '导出诊断包 (zip)' }}
      </RButton>
    </div>

    <p v-if="loadError" class="diag-error" role="alert">{{ loadError }}</p>

    <template v-if="preview">
      <dl class="diag-manifest">
        <div v-for="row in manifestRows" :key="row.label" class="diag-row">
          <dt class="diag-key">{{ row.label }}</dt>
          <dd class="diag-val tnum">{{ row.value }}</dd>
        </div>
      </dl>

      <details v-if="preview.log_sample.length" class="diag-logs">
        <summary>日志样本（最近 {{ preview.log_sample.length }} 行）</summary>
        <pre class="diag-log">{{ preview.log_sample.join('\n') }}</pre>
      </details>

      <p v-if="preview.redaction" class="diag-redaction">{{ preview.redaction }}</p>
    </template>

    <REmpty
      v-else-if="!loadError && !loading"
      face="waiting"
      title="还没看过诊断信息"
      hint="点「查看诊断信息」看看当前这台机器的状态，也可以直接导出诊断包。"
    />
  </div>
</template>

<style scoped>
.diag-desc { font-size: var(--font-body); color: var(--text-3); margin-bottom: 12px; line-height: 1.7; }
.diag-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.diag-error { margin-top: 10px; font-size: var(--font-aux); color: var(--danger); }
.diag-manifest { margin: 14px 0 0; display: flex; flex-direction: column; gap: 6px; }
.diag-row { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; }
.diag-key { font-size: var(--font-aux); color: var(--text-3); min-width: 72px; }
.diag-val { font-size: var(--font-body); color: var(--text-1); margin: 0; }
.diag-logs { margin-top: 14px; }
.diag-logs summary { font-size: var(--font-aux); color: var(--text-2); cursor: pointer; }
.diag-log {
  margin-top: 8px; padding: 10px 12px; background: var(--surface-2);
  border-radius: var(--radius-ctrl); font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: var(--font-aux); color: var(--text-2); max-height: 320px; overflow: auto;
  white-space: pre-wrap; word-break: break-all;
}
.diag-redaction { margin-top: 10px; font-size: var(--font-aux); color: var(--text-3); }
</style>
