<script lang="ts">
// RDialog：破坏性操作确认对话框（docs/07 §4「仅破坏性决策；默认焦点「取消」；≤480px」）。
//
// 安全设计（这是本组件存在的主要理由）：
// 1) 打开后焦点必须落在「取消」上。用户看到对话框时，回车/空格默认触发的是「取消」，
//    而不是直接执行删除。要执行破坏性动作必须再主动 Tab 到「确认」并按下——
//    误触（手滑回车、习惯性回车）不会造成不可逆后果。
// 2) 标题与确认按钮在 danger 模式下走 danger 色：破坏性动作在最后一米必须显眼。
// 3) Esc 与点击遮罩都关闭：用户永远有零成本的退出路径，不会被困在对话框里。
// 4) 打开时锁 body 滚动：背景页面不能被滚走，否则用户失去「自己在哪」的坐标感。
// 5) 宽度上限 480px：确认框是短文本场景，超过 480px 会变成一张看不懂的长表单。
//
// 滚动锁放在模块级（普通 script 块）而不是 script setup 里：
// script setup 的顶层代码是「每实例一份」，两个对话框同时打开时，
// 各自把 body 的 overflow 还原成 '' 会在还有一个对话框开着时提前解锁。
// 共享计数器保证最后一个对话框关闭时才真正解锁。
let scrollLockCount = 0
let scrollLockPrev = ''

function lockBodyScroll(): void {
  if (scrollLockCount === 0) {
    scrollLockPrev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
  scrollLockCount += 1
}

function unlockBodyScroll(): void {
  if (scrollLockCount === 0) return
  scrollLockCount -= 1
  if (scrollLockCount === 0) document.body.style.overflow = scrollLockPrev
}

/**
 * 当前打开的对话框栈（后进先出）。
 * Esc 只应该关闭最上面那个：叠了两层对话框时按一次 Esc 不该把两层一起关掉，
 * 否则用户会莫名其妙地一次退出两级流程。
 */
const openDialogs: symbol[] = []
</script>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import RButton from './RButton.vue'

const props = withDefaults(
  defineProps<{
    /** 是否打开。 */
    open: boolean
    /** 对话框标题。 */
    title: string
    /** 正文说明。 */
    description?: string
    /** 破坏性模式：标题与确认按钮走 danger 色。 */
    danger?: boolean
    /** 确认按钮文案。 */
    confirmText?: string
    /** 取消按钮文案。 */
    cancelText?: string
    /** 确认按钮禁用（例如请求进行中）。 */
    confirmDisabled?: boolean
  }>(),
  {
    description: '',
    danger: false,
    confirmText: '确认',
    cancelText: '取消',
    confirmDisabled: false,
  },
)

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void
  (e: 'confirm'): void
  (e: 'cancel'): void
}>()

defineSlots<{
  /** 对话框正文补充内容。 */
  default(): unknown
}>()

const panelEl = ref<HTMLElement | null>(null)
/** 打开前的焦点元素：关闭后要还回去，否则用户会「丢失」焦点位置。 */
let lastFocused: HTMLElement | null = null
/**
 * 本实例是否真的持有滚动锁。
 * 滚动锁计数是模块级共享的，所以「解锁」必须和「本实例加过锁」严格配对：
 * 否则一个从未打开过的对话框（挂载时 open=false）会在初始化时把别人的锁解开，
 * 一个已关闭的对话框被卸载时也会多解一次。
 */
let holdsLock = false
/** 本实例在「已打开对话框栈」里的身份，用来判断自己是不是最上面那层。 */
const instanceId = Symbol('rdialog')

const cls = computed(() => ['rdialog', { 'rdialog--danger': props.danger }])

/** 查找面板内可聚焦元素。 */
function focusables(): HTMLElement[] {
  if (!panelEl.value) return []
  return Array.from(
    panelEl.value.querySelectorAll<HTMLElement>(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
    ),
  ).filter((el) => !el.hasAttribute('disabled'))
}

/**
 * 打开时：记下原焦点 → 锁滚动 → 聚焦「取消」。
 * 解锁分支必须判断 holdsLock，否则「本实例从没开过」也会去动共享计数。
 */
watch(
  () => props.open,
  async (open) => {
    if (open) {
      lastFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null
      if (!holdsLock) {
        lockBodyScroll()
        holdsLock = true
      }
      // 压栈：Esc 只由最上面那层响应。
      openDialogs.push(instanceId)
      document.addEventListener('keydown', onKeydown)
      await nextTick()
      // 硬性要求：默认焦点落在「取消」上。第一个可聚焦元素就是取消按钮
      // （取消在 DOM 顺序上排在确认之前，且这里显式按 data 属性再确认一次）。
      const buttons = focusables()
      const cancel = buttons.find((b) => b.dataset.rdialogAction === 'cancel') ?? buttons[0]
      cancel?.focus()
    } else {
      if (holdsLock) {
        unlockBodyScroll()
        holdsLock = false
      }
      const i = openDialogs.indexOf(instanceId)
      if (i >= 0) openDialogs.splice(i, 1)
      document.removeEventListener('keydown', onKeydown)
      lastFocused?.focus()
      lastFocused = null
    }
  },
  { immediate: true },
)

/** 组件被直接卸载时（父级 v-if 掉整个对话框）也要把滚动锁和监听解开。 */
onBeforeUnmount(() => {
  if (holdsLock) {
    unlockBodyScroll()
    holdsLock = false
  }
  const i = openDialogs.indexOf(instanceId)
  if (i >= 0) openDialogs.splice(i, 1)
  document.removeEventListener('keydown', onKeydown)
  lastFocused?.focus()
  lastFocused = null
})

/**
 * 键盘处理。
 * 只在最上面那层对话框里生效：叠了两层时按 Esc 只关最上面一层，
 * 否则用户会一次退出两级流程。Tab 焦点环绕同理。
 */
function onKeydown(e: KeyboardEvent) {
  if (openDialogs[openDialogs.length - 1] !== instanceId) return
  if (e.key === 'Escape') {
    e.stopPropagation()
    close()
    return
  }
  // Tab 焦点环绕：焦点不能跑到对话框外面去，否则读屏用户会掉进背景内容里。
  if (e.key !== 'Tab') return
  const items = focusables()
  if (items.length === 0) return
  const first = items[0]
  const last = items[items.length - 1]
  const active = document.activeElement
  if (e.shiftKey && active === first) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && active === last) {
    e.preventDefault()
    first.focus()
  }
}

function close() {
  emit('update:open', false)
  emit('cancel')
}

function confirm() {
  emit('confirm')
}
</script>

<template>
  <Teleport to="body">
    <Transition name="rdialog">
      <!-- 键盘事件统一在 document 上监听（见 onKeydown）：
           这里再绑一次 @keydown 会让同一次按键被处理两遍（冒泡到根节点一次、到 document 一次），
           Esc 就会重复 emit cancel。焦点即使跑出对话框，document 监听依然能兜住 Esc。 -->
      <div v-if="open" class="rdialog-root" role="dialog" aria-modal="true">
        <div class="rdialog-mask" @click="close" />
        <div ref="panelEl" :class="cls">
          <h3 class="rdialog-title">{{ title }}</h3>
          <p v-if="description" class="rdialog-desc">{{ description }}</p>
          <div class="rdialog-body">
            <slot />
          </div>
          <footer class="rdialog-foot">
            <RButton
              data-rdialog-action="cancel"
              variant="outline"
              @click="close"
            >
              {{ cancelText }}
            </RButton>
            <RButton
              data-rdialog-action="confirm"
              variant="accent"
              :tone="danger ? 'danger' : 'default'"
              :disabled="confirmDisabled"
              @click="confirm"
            >
              {{ confirmText }}
            </RButton>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.rdialog-root {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}
/* 遮罩色取 tokens.css 的 --scrim：抽屉 / 对话框 / 汉堡导航 /「更多」面板
   四个遮罩必须同源，否则会出现「同一操作两处深浅不一样」。
   早先这里用 color-mix(var(--text-1) 32%) 现算，而 DetailDrawer/App 用 var(--scrim)，
   两套算法在深浅两模式下都各自漂移；且 --scrim 当初只定义在 [data-theme='dark']，
   浅色模式下现算的这版反而成了唯一能看见的那个——同源之后才谈得上可调。 */
.rdialog-mask {
  position: absolute;
  inset: 0;
  background: var(--scrim);
}
.rdialog {
  position: relative;
  width: 100%;
  max-width: 480px;
  padding: 20px;
  border-radius: var(--radius-modal);
  background: var(--surface);
  box-shadow: var(--shadow-3);
}
.rdialog-title {
  margin: 0 0 8px;
  font-size: var(--font-title-3);
  font-weight: var(--weight-strong);
  color: var(--text-1);
}
/* 破坏性模式：标题走 danger 色，最后一公里必须显眼。 */
.rdialog--danger .rdialog-title {
  color: var(--danger);
}
.rdialog-desc {
  margin: 0;
  font-size: var(--font-aux);
  color: var(--text-2);
  line-height: 1.6;
}
.rdialog-body {
  margin-top: 12px;
  font-size: var(--font-aux);
  color: var(--text-1);
}
.rdialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 20px;
}
/* 面板抽屉动效：180–240ms cubic-bezier(.2,.8,.2,1)（docs/07 §6）。 */
.rdialog-enter-active,
.rdialog-leave-active {
  transition: opacity 200ms cubic-bezier(0.2, 0.8, 0.2, 1);
}
.rdialog-enter-active .rdialog,
.rdialog-leave-active .rdialog {
  transition: transform 200ms cubic-bezier(0.2, 0.8, 0.2, 1);
}
.rdialog-enter-from,
.rdialog-leave-to {
  opacity: 0;
}
.rdialog-enter-from .rdialog,
.rdialog-leave-to .rdialog {
  transform: translateY(8px) scale(0.98);
}
</style>
