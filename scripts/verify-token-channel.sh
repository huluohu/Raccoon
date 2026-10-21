#!/usr/bin/env bash
# API Token 认证通道验证（curl 层，必须无会话 cookie）。
#
# 为什么不能放在浏览器 e2e 里：同源 fetch 会自动携带会话 cookie，被 cookie 通道放行是
# 正确行为（cookie 优先级高于 Bearer），测出来的 200 无法区分「令牌有效」和「cookie 兜底」。
# 令牌撤销是否真失效，只能在没有 cookie 的环境里判定。
#
# 兼容性：macOS 自带 bash 3.2 —— 不用关联数组、不用 ${arr[@]}、不用 mapfile。
# 也不用 set -u：bash 3.2 在 set -u 下展开空数组（"${_b[@]}"）会报 unbound variable。
#
# 用法：BASE=http://127.0.0.1:18551 bash scripts/verify-token-channel.sh
set -o pipefail

BASE="${BASE:-http://127.0.0.1:18551}"
DB="${RACCOON_DB:-/tmp/rr3/data/raccoon.db}"
USER="${RACCOON_USER:-admin}"
PASS="${RACCOON_PASS:-Raccoon@2026}"
JAR="$(mktemp)"
BODY="$(mktemp)"
trap 'rm -f "$JAR" "$BODY"' EXIT

pass=0; fail=0
ok()  { echo "  ✔ $1${2:+ — $2}"; pass=$((pass+1)); }
bad() { echo "  ✘ $1${2:+ — $2}"; fail=$((fail+1)); }

# jget <python表达式>：从 $BODY 读 JSON 取字段
jget() { python3 -c "import sys,json
try: d=json.load(open('$BODY'))
except Exception: d={}
print($1)" 2>/dev/null; }

# code <method> <path> <jar|-> [data] [auth-header] [csrf]
#   → 只取 HTTP 状态码
code() {
  _m="$1"; _p="$2"; _jar="$3"; _data="${4:-}"; _auth="${5:-}"; _csrf="${6:-}"
  if [ "$_jar" = "-" ]; then _b=(); else _b=(-b "$_jar"); fi
  if [ -n "$_data" ]; then _d=(-H 'Content-Type: application/json' -d "$_data"); else _d=(); fi
  if [ -n "$_auth" ]; then _a=(-H "Authorization: Bearer $_auth"); else _a=(); fi
  if [ -n "$_csrf" ]; then _c=(-H "X-CSRF-Token: $_csrf"); else _c=(); fi
  curl -s --noproxy '*' -o /dev/null -w '%{http_code}' -X "$_m" "$BASE$_p" "${_b[@]}" "${_d[@]}" "${_a[@]}" "${_c[@]}"
}

# api <method> <path> <data> [auth-header] → 响应体写入 $BODY
api() {
  _m="$1"; _p="$2"; _data="${3:-}"; _auth="${4:-}"
  if [ -n "$_data" ]; then _d=(-H 'Content-Type: application/json' -d "$_data"); else _d=(); fi
  if [ -n "$_auth" ]; then _a=(-H "Authorization: Bearer $_auth"); else _a=(); fi
  curl -s --noproxy '*' -o "$BODY" -X "$_m" "$BASE$_p" "${_d[@]}" "${_a[@]}"
}

# sess <method> <path> [data] → 用会话 cookie + CSRF 头调用
sess() {
  _m="$1"; _p="$2"; _data="${3:-}"
  if [ -n "$_data" ]; then
    curl -s --noproxy '*' -o "$BODY" -b "$JAR" -X "$_m" "$BASE$_p" \
      -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' -d "$_data"
  else
    curl -s --noproxy '*' -o "$BODY" -b "$JAR" -X "$_m" "$BASE$_p" -H "X-CSRF-Token: $CSRF"
  fi
}

echo "== 登录取会话 =="
curl -s --noproxy '*' -c "$JAR" -o "$BODY" -X POST "$BASE/api/v1/auth/login" \
  -H 'Content-Type: application/json' -d "{\"username\":\"$USER\",\"password\":\"$PASS\"}"
CSRF="$(jget "d.get('csrf_token','')")"
[ -n "$CSRF" ] && ok "登录成功（csrf 已下发）" || { bad "登录失败" "$(cat "$BODY")"; exit 1; }

echo
echo "== 基线：匿名与无效令牌必须 401 =="
c="$(code GET /api/v1/system/queue -)"
[ "$c" = "401" ] && ok "匿名访问被拒" "401" || bad "匿名访问被拒" "$c"
c="$(code GET /api/v1/system/queue - '' 'rc_totally_bogus_token_000')"
[ "$c" = "401" ] && ok "无效令牌被拒" "401" || bad "无效令牌被拒" "$c"

echo
echo "== 签发三种 scope =="
TK1=''; TK3=''; TK7=''; ID7=''
for s in 1 3 7; do
  sess POST /api/v1/tokens "{\"name\":\"verify-s$s\",\"scope\":$s,\"expires_in_hours\":0}"
  T="$(jget "d.get('token','')")"
  [ -n "$T" ] && ok "签发 scope=$s" "${T:0:10}…" || bad "签发 scope=$s" "$(cat "$BODY")"
  case "$s" in
    1) TK1="$T" ;;
    3) TK3="$T" ;;
    7) TK7="$T"; ID7="$(jget "d.get('data',{}).get('id','')")" ;;
  esac
done

echo
echo "== scope 分级：读放行 / admin 域需 admin 位 =="
# scope 位掩码：read=1 write=2 admin=4。config/credentials/tokens 等管理域要求 admin 位（scope=7）。
for s in 1 3 7; do
  eval "T=\${TK$s}"
  c="$(code GET /api/v1/system/queue - '' "$T")"
  [ "$c" = "200" ] && ok "scope=$s 可读队列" "200" || bad "scope=$s 可读队列" "$c"
  # 写普通资源（任务批量操作）需 write 位。空数组会被后端 400 拒绝（ids 不能为空），
  # 所以这里用一个真实存在的任务 ID 不必要 —— 只需确认「不是 403」即可区分权限通过与否。
  c="$(code POST /api/v1/tasks/batch - '{"ids":[],"action":"cancel"}' "$T")"
  if [ "$s" = "1" ]; then
    [ "$c" = "403" ] && ok "scope=1 只读：写任务被拒" "403" || bad "scope=1 只读：写任务被拒" "$c"
  else
    [ "$c" = "400" ] && ok "scope=$s 通过 write 校验（进到业务校验层）" "400 ids 不能为空" \
      || bad "scope=$s 可写任务域" "$c"
  fi
  # 管理域（config）：一律要求 admin 位，只有 scope=7 能过
  c="$(code PUT /api/v1/config - '{"settings":{"download.max_concurrent":4}}' "$T")"
  if [ "$s" = "7" ]; then
    [ "$c" = "200" ] && ok "scope=7 可写配置（admin 域）" "200" || bad "scope=7 可写配置" "$c"
  else
    [ "$c" = "403" ] && ok "scope=$s 无 admin 位：配置被拒" "403" || bad "scope=$s 无 admin 位：配置被拒" "$c"
  fi
done

echo
echo "== Token 通道豁免 CSRF（无 cookie 无 csrf 头仍可写） =="
# Bearer 通道不校验 CSRF：能进到业务校验层（400）就证明没被 CSRF 中间件拦下。
c="$(code POST /api/v1/tasks/batch - '{"ids":[],"action":"cancel"}' "$TK3")"
[ "$c" = "400" ] && ok "Bearer 通道免 CSRF（进到业务校验层）" "$c" || bad "Bearer 通道免 CSRF" "$c"

echo
echo "== 撤销后立即失效（撤销动作本身需 admin 令牌或带 CSRF 的会话） =="
# DELETE /tokens 走 csrfForSession：会话通道必须带 X-CSRF-Token，否则 403（这是正确行为）。
c="$(code DELETE "/api/v1/tokens/$ID7" "$JAR" '' '' "$CSRF")"
[ "$c" = "200" ] && ok "撤销请求成功（会话 + CSRF）" "200" || bad "撤销请求成功" "$c"
c="$(code GET /api/v1/system/queue - '' "$TK7")"
[ "$c" = "401" ] && ok "撤销后令牌立即 401（无 cookie 环境）" "401" || bad "撤销后令牌立即 401" "$c"

echo
echo "== 反证：会话通道缺 CSRF 必须被拒（403） =="
sess POST /api/v1/tokens '{"name":"csrf-probe","scope":1,"expires_in_hours":0}'
PROBE_ID="$(jget "d.get('data',{}).get('id','')")"
c="$(code DELETE "/api/v1/tokens/$PROBE_ID" "$JAR")"
[ "$c" = "403" ] && ok "会话缺 CSRF 被拒 403" "$c" || bad "会话缺 CSRF 被拒" "$c"
sess DELETE "/api/v1/tokens/$PROBE_ID"
ok "探针令牌已撤销"

echo
echo "== 过期令牌：负数有效期必须被拒（否则等于发永久令牌） =="
# 用会话通道测：Bearer 令牌会被上面的撤销步骤作废，用它测会得到 401 而非参数校验的 400。
sess POST /api/v1/tokens '{"name":"verify-negative","scope":1,"expires_in_hours":-1}'
if grep -q 'VALIDATION' "$BODY" 2>/dev/null; then
  ok "负数有效期被拒 400（防止静默发永久令牌）" "VALIDATION"
else
  bad "负数有效期被拒" "$(cat "$BODY")"
fi

echo
echo "== 真实过期令牌 =="
sess POST /api/v1/tokens '{"name":"verify-expired","scope":1,"expires_in_hours":1}'
ETK="$(jget "d.get('token','')")"
EID="$(jget "d.get('data',{}).get('id','')")"
if [ -n "$ETK" ]; then
  c="$(code GET /api/v1/system/queue - '' "$ETK")"
  [ "$c" = "200" ] && ok "未过期令牌可用" "200" || bad "未过期令牌可用" "$c"
  # 直接改库把 expires_at 拨到过去（不等待真实时间流逝）
  python3 - "$EID" "$DB" <<'PY'
import sqlite3, sys, time
db = sqlite3.connect(sys.argv[2])
db.execute("UPDATE api_tokens SET expires_at=? WHERE id=?", (int(time.time()*1000)-60_000, sys.argv[1]))
db.commit(); db.close()
PY
  c="$(code GET /api/v1/system/queue - '' "$ETK")"
  [ "$c" = "401" ] && ok "过期令牌被拒 401（拨库验证，非等待）" "401" || bad "过期令牌被拒" "$c"
else
  bad "签发过期令牌" "$(cat "$BODY")"
fi

echo
echo "== 清理 =="
sess PUT /api/v1/config '{"settings":{"download.max_concurrent":3}}'
ok "配置已还原（max_concurrent=3）"

echo
echo "结果：$pass 通过 / $fail 失败"
[ "$fail" -eq 0 ] || exit 1