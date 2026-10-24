# 引擎输出 Golden Fixture

这里的 `.stderr` 文件是**从真实 yt-dlp 录制的原始输出**，不是手写的假字符串。

`classify`（错误分类）的输入就是引擎的 stderr。手写字符串覆盖不到引擎真实措辞的
细微差别——比如 `This video is unavailable`（B 站/YouTube 实际输出）与
`Video unavailable`（老版本）之差，早先就因为只匹配后者而漏判成 `ENGINE_ERROR`。

引擎改版导致输出变化时，`golden_test.go` 会先红——这是提醒：分类规则需要同步更新，
而不是让一条陈旧的分类规则继续骗人。

## 录制方法

fixture 用 yt-dlp 2026.08.19 录制，命令在下面（换 URL 即可录新场景）：

```bash
# 视频不存在
yt-dlp --no-warnings --socket-timeout 20 --dump-single-json \
  "https://www.youtube.com/watch?v=aaaaaaaaaaa" 2> ytdlp_not_found.stderr

# 私有/不可用视频
yt-dlp --no-warnings --socket-timeout 20 --dump-single-json \
  "https://www.youtube.com/watch?v=BaW_jenozKc" 2> ytdlp_private.stderr

# 格式不可用
yt-dlp --no-warnings --socket-timeout 20 -f "99999p" \
  "https://www.youtube.com/watch?v=dQw4w9WgXcQ" 2> ytdlp_format_unavailable.stderr
```

## 纪律

- **只录真实输出**。手写的假字符串留在 `classify_test.go`（那里测的是规则边界，
  允许构造输入；这里测的是「真实引擎长什么样」）。
- **录完更新断言**。新 fixture 必须挂进 `golden_test.go` 的用例表，
  断言分类结果（Code + Detail 关键片段），不要断言 stderr 全文——全文会随引擎版本变。
- **不录含凭据的输出**。带 Cookie 的输出里可能有会话信息，录制前确认 URL 是公开内容。
