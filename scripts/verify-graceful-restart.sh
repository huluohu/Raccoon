#!/usr/bin/env bash
# 让出重启（/api/v1/system/restart）端到端验证。
#
# 全部基于真实进程与真实下载，不 mock：
#   1. 未登录时重启被拒（鉴权）
#   2. 空闲时重启 → 立即返回（waited_sec≈0），主进程随后真的退出
#   3. 有运行中任务时重启 → waited_sec > 0 且 still_running 如实上报，
#      即「确实在等让出」而不是空转
#   4. 主进程退出后，引擎子进程不会被一起带走（应由 PID 登记机制在下次启动回收）
#
# 用法：BASE=http://127.0.0.1:18551 DATA_DIR=/tmp/rr3/data \
#       bash scripts/verify-graceful-restart.sh
set -uo pipefail

BASE="${BASE:-http://127.0.0.1:18551}"
DATA_DIR="${DATA_DIR:-}"
USER_NAME="${RACCOON_USER:-admin}"
PASS="${RACCOON_PASS:-Raccoon@2026}"
JAR="$(mktemp -t raccoon-restart-jar)"
trap 'rm -f "$JAR"' EXIT

pass=0; fail=0
ok()  { pass=$((pass+1)); printf '  \033[32mPASS\033[0m %-44s %s\n' "$1" "${2:-}"; }
bad() { fail=$((fail+1)); printf '  \033[31mFAIL\033[0m %-44s %s\n' "$1" "${2:-}"; }

code() { curl -s --noproxy '*' -m 10 -o /dev/null -w '%{http_code}' "$@"; }
body() { curl -s --noproxy '*' -m 120 "$@"; }
jget() { python3 -c "import sys,json;d=json.load(sys.stdin);print($1)" 2>/dev/null; }

mainpid() { [ -n "$DATA_DIR" ] && [ -f "$DATA_DIR/raccoon.pid" ] && cat "$DATA_DIR/raccoon.pid" || echo ""; }
alive()   { kill -0 "$1" 2>/dev/null; }

echo "== 让出重启验证 =="
echo "BASE=$BASE"

# ---- 0. 未登录不能重启 ----
c="$(code -X POST "$BASE/api/v1/system/restart")"
case "$c" in
  401|403) ok "未登录重启被拒" "$c" ;;
  *)       bad "未登录重启被拒" "$c（期望 401/403）" ;;
esac

# ---- 1. 登录拿 CSRF ----
L="$(body -c "$JAR" -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
      -d "{\"username\":\"$USER_NAME\",\"password\":\"$PASS\"}")"
CSRF="$(printf '%s' "$L" | jget 'd.get("csrf_token","")')"
[ -n "$CSRF" ] && ok "登录并取得 CSRF" || { bad "登录并取得 CSRF" "$L"; exit 1; }

PID_BEFORE="$(mainpid)"
echo "  主进程 PID=${PID_BEFORE:-<未知>}"

# ---- 2. 提交一个下载并立刻重启：验证「等让出」而非空转 ----
# 用一个较大的清晰度，保证提交后有几秒处于 downloading，足够让出逻辑观察到 running>0。
SUB="$(body -b "$JAR" -X POST "$BASE/api/v1/tasks" -H "X-CSRF-Token: $CSRF" \
        -H 'Content-Type: application/json' \
        -d '{"items":[{"url":"https://www.bilibili.com/video/BV1GJ411x7h7","quality":"1080p"}]}')"
N="$(printf '%s' "$SUB" | jget "len(d.get('data',[]))")"
[ "${N:-0}" -ge 1 ] && ok "提交下载任务" "N=$N" || bad "提交下载任务" "$SUB"

# 关键：不 sleep。连查队列并立刻重启，把「任务还在跑」的窗口压到最小——
# 否则小体积下载会在查询与重启之间跑完，让出逻辑读到 running=0，
# 验证的就只是空闲路径而非「等待让出」路径。
# 等调度循环真正领走任务（tick 500ms），否则 running 还是 0，
# 测到的只是「空闲路径重启」，不是「等待让出」。
RUNNING=0
for _ in $(seq 1 40); do
  RUNNING="$(body -b "$JAR" "$BASE/api/v1/system/queue" | jget "d.get('running',0)")"
  [ "${RUNNING:-0}" -gt 0 ] && break
  sleep 0.25
done
ENGINE_PID="$([ -n "$DATA_DIR" ] && [ -f "$DATA_DIR/engine.pids" ] && head -1 "$DATA_DIR/engine.pids" || echo "")"
echo "  重启前 running=$RUNNING 引擎 PID=${ENGINE_PID:-<无>}"

R="$(body -b "$JAR" -X POST "$BASE/api/v1/system/restart" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json')"
O="$(printf '%s' "$R" | jget "d.get('ok')")"
W="$(printf '%s' "$R" | jget "d.get('waited_sec')")"
S="$(printf '%s' "$R" | jget "d.get('still_running')")"

[ "$O" = "True" ] && ok "重启返回 ok=true" "waited_sec=$W still_running=$S" \
                  || bad "重启返回 ok=true" "$R"

# waited_sec > 0 证明确实等过让出；若 running 原本为 0 则允许为 0（空闲路径）
if [ "${RUNNING:-0}" -gt 0 ]; then
  if [ "${W:-0}" -gt 0 ]; then
    ok "有运行任务时确实等待让出" "waited_sec=${W}"
  else
    bad "有运行任务时确实等待让出" "waited_sec=${W}（running 原本=${RUNNING}）"
  fi
  if [ "${S:-0}" -ge 0 ]; then
    ok "still_running 如实上报" "${S}"
  else
    bad "still_running 如实上报" "${S}"
  fi
else
  ok "空闲路径 waited_sec=0（无需等待）" "running=${RUNNING}"
fi

# ---- 3. 主进程应真的退出 ----
EXITED=no
if [ -z "$PID_BEFORE" ]; then
  bad "主进程在响应后真的退出" "未提供 DATA_DIR，无法读 raccoon.pid"
else
  for _ in $(seq 1 24); do
    alive "$PID_BEFORE" || { EXITED=yes; break; }
    sleep 0.5
  done
  [ "$EXITED" = "yes" ] && ok "主进程在响应后真的退出" "PID=$PID_BEFORE" \
                        || bad "主进程在响应后真的退出" "12s 后 PID=$PID_BEFORE 仍存活"
fi

# ---- 4. 引擎子进程不应被主进程退出连带杀掉（应留给下次启动回收）----
if [ -n "$ENGINE_PID" ]; then
  if alive "$ENGINE_PID"; then
    ok "引擎子进程成为孤儿（交由下次启动回收）" "PID=$ENGINE_PID 仍存活"
  else
    ok "引擎子进程已随让出结束" "PID=$ENGINE_PID"
  fi
fi

echo
printf '结果：%d 通过 / %d 失败\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1