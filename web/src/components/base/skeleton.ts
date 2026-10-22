// 骨架屏：加载态专用（docs/07 §4「空态 ≠ 骨架屏」）。
//
// 为什么单独成文件：骨架屏和「无数据」空态是两种互斥状态，必须在代码上分开表达，
// 否则很容易出现「正在加载却显示『暂无数据』」这种骗人的界面。
// 这里只放骨架的形状计算，具体的灰块样式由使用方（组件 scoped CSS）负责，
// 颜色仍然取自 tokens.css 的 CSS 变量。

/** 默认骨架行数：够占位又不至于把首屏刷得太满。 */
export const SKELETON_ROW_COUNT = 3

/**
 * 某一行的各列占位宽度（百分比）。
 * 刻意做成不等宽：完全等宽的灰条看起来像真数据，反而误导用户以为已经有内容了。
 * @param count 列数
 * @param rowIndex 行序号，用来让不同行的抖动错开
 */
export function skeletonWidths(count: number, rowIndex = 0): number[] {
  const widths: number[] = []
  for (let i = 0; i < count; i += 1) {
    const jitter = (i * 17 + rowIndex * 29) % 24
    widths.push(58 + jitter)
  }
  return widths
}
