// 共享秒级时钟：倒计时（自动重试 Ns 后）多个组件共用一个 interval，
// 不给列表里每一行单开定时器。引用计数：首个使用者在挂载时启动，最后一个卸载时停。
import { onBeforeUnmount, onMounted, ref } from 'vue'

const now = ref(Date.now())
let timer: number | undefined
let users = 0

function start(): void {
  users += 1
  if (timer === undefined) {
    timer = window.setInterval(() => {
      now.value = Date.now()
    }, 1000)
  }
}

function stop(): void {
  users -= 1
  if (users <= 0 && timer !== undefined) {
    window.clearInterval(timer)
    timer = undefined
  }
}

/** 每秒跳动的当前时间（毫秒）。只在意时间流动的组件里订阅； SSR/测试环境安全。 */
export function useNowTick(): Readonly<typeof now> {
  onMounted(start)
  onBeforeUnmount(stop)
  return now
}
