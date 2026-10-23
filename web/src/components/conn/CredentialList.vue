<script setup lang="ts">
// CredentialList：凭据列表（方案 §3「/connections → CredentialList（打码）」）。
//
// 为什么抽成组件：
// 1) 「前端永远拿不到明文，只能拿到打码预览」是这一页的铁律。
//    预览值的展示（等宽字体 + 单独一行）必须和「不提供明文回显」这件事绑在一起，
//    抽出来就不会被后来的一次改动顺手加个「显示明文」按钮。
// 2) 每张凭据卡有固定的信息结构（状态点 + 类型徽章 → 标签 → 三项元信息 →
//    健康检查结果 → 三个动作），拆出来结构有明确归属。
import PlatformBadge from '@/components/task/PlatformBadge.vue'
import RButton from '@/components/base/RButton.vue'
import { formatRelative } from '@/utils/format'
import type { Credential } from '@/api/types'

defineProps<{
  /** 凭据列表。 */
  items: Credential[]
  /** 正在做健康检查的凭据 ID。 */
  checking?: string
  /** 健康检查结果缓存：id → { ok, message }。 */
  results?: Record<string, { ok: boolean; status?: string; message: string }>
}>()

const emit = defineEmits<{
  /** 健康检查。 */
  check: [cred: Credential]
  /** 编辑。 */
  edit: [cred: Credential]
  /** 删除。 */
  remove: [cred: Credential]
}>()

/** 凭据状态 → 中文。 */
const STATUS_TEXT: Record<string, string> = {
  active: '可用',
  expiring: '待确认',
  expired: '已失效',
  disabled: '已停用',
}

/** 凭据状态 → 语义色。 */
const STATUS_TONE: Record<string, string> = {
  active: 'var(--success)',
  expiring: 'var(--warning)',
  expired: 'var(--danger)',
  disabled: 'var(--neutral)',
}

/** 凭据类型 → 中文。 */
const KIND_TEXT: Record<string, string> = {
  cookie_file: 'Cookie',
  account: '账号',
  token: '令牌',
  session: '会话',
}

/** 到期时间 → 人话。 */
function expireText(c: Credential): string {
  if (!c.expires_at) return '长期有效'
  const left = c.expires_at - Date.now()
  if (left <= 0) return '已过期 ' + formatRelative(c.expires_at)
  return formatRelative(c.expires_at) + '过期'
}
</script>

<template>
  <div class="cred-grid">
    <article v-for="c in items" :key="c.id" class="card">
      <header class="card-head">
        <PlatformBadge :platform="c.platform" />
        <span
          class="dot"
          :style="{ background: STATUS_TONE[c.status] || 'var(--neutral)' }"
          aria-hidden="true"
        />
        <span class="status" :style="{ color: STATUS_TONE[c.status] }">
          {{ STATUS_TEXT[c.status] || c.status }}
        </span>
        <span class="kind">{{ KIND_TEXT[c.kind] || c.kind }}</span>
      </header>

      <h3 class="card-label">{{ c.label }}</h3>

      <dl class="meta">
        <div><dt>内容预览</dt><dd class="mono">{{ c.preview || '—' }}</dd></div>
        <div><dt>有效期</dt><dd>{{ expireText(c) }}</dd></div>
        <div>
          <dt>上次检查</dt>
          <dd>{{ c.last_check_at ? formatRelative(c.last_check_at) : '未检查' }}</dd>
        </div>
      </dl>

      <p v-if="c.note" class="note">{{ c.note }}</p>
      <p v-if="results?.[c.id]" class="check" :class="results[c.id].ok ? 'ok' : 'bad'">
        {{ results[c.id].ok ? '✓' : '!' }} {{ results[c.id].message }}
      </p>

      <footer class="card-foot">
        <RButton size="sm" :disabled="checking === c.id" @click="emit('check', c)">
          {{ checking === c.id ? '检查中…' : '健康检查' }}
        </RButton>
        <RButton size="sm" variant="ghost" @click="emit('edit', c)">编辑</RButton>
        <RButton
          size="sm"
          variant="ghost"
          tone="danger"
          confirm="确认删除？"
          @click="emit('remove', c)"
        >
          删除
        </RButton>
      </footer>
    </article>
  </div>
</template>

<style scoped>
/* 凭据网格：auto-fit 让空轨道塌陷（auto-fill 会给 1 个凭据也留 3 条轨道）。 */
.cred-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 16px; }
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-card);
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  box-shadow: var(--shadow-1);
}
.card-head { display: flex; align-items: center; gap: 8px; }
.dot { width: 8px; height: 8px; border-radius: 50%; margin-left: auto; }
/* 状态徽章是辅助信息，caption(12px) 合规。 */
.status { font-size: var(--font-caption); font-weight: var(--weight-semibold); }
.kind {
  font-size: var(--font-caption);
  color: var(--text-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 1px 8px;
}
.card-label { font-size: var(--font-title-3); font-weight: var(--weight-medium); }
.meta { display: flex; flex-direction: column; gap: 6px; }
.meta > div { display: flex; gap: 10px; font-size: var(--font-aux); }
.meta dt { color: var(--text-3); width: 72px; flex-shrink: 0; }
.meta dd { color: var(--text-2); }
/* 打码预览：等宽字体，和普通文本明显区分——这是「这是被遮过的内容」的视觉信号。 */
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.note { font-size: var(--font-aux); color: var(--text-3); }
.check { font-size: var(--font-aux); }
.check.ok { color: var(--success); }
.check.bad { color: var(--warning); }
.card-foot {
  display: flex;
  gap: 6px;
  margin-top: auto;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}
</style>
