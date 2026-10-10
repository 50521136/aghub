#!/usr/bin/env bash
# 验证「网页向导走完后门户是否立刻可用」。
#
# 关键点：向导是在**同一个进程**里完成的，所以测完绝对不能重启 —— 一重启
# initPortal 就会由 !isFirstRun 分支补上，bug 被掩盖，测试就失去意义。
#
# 对两个二进制各跑一遍：旧版（v1.0.38，预期 /portal/ 404）和新版（预期 200）。
set -uo pipefail

WEB_PORT=13456
DNS_PORT=15353
BASE="http://127.0.0.1:$WEB_PORT"

run_case() {
  local label="$1" bin="$2" dir="$3"

  echo "=============================================================="
  echo "  $label"
  echo "  binary: $bin"
  echo "=============================================================="

  rm -rf "$dir"; mkdir -p "$dir"
  cd "$dir" || return 1

  "$bin" -w "$dir" --web-addr "127.0.0.1:$WEB_PORT" -s run > "$dir/run.log" 2>&1 &
  local pid=$!
  echo "  PID=$pid"

  # 等首启模式起来（install.html 能应答）
  local i code
  for i in $(seq 1 30); do
    sleep 1
    code=$(curl -s -o /dev/null -w '%{http_code}' -m 3 "$BASE/install.html" 2>/dev/null)
    [ "$code" = "200" ] && break
  done
  echo "  install.html -> ${code:-无响应}"

  if [ "${code:-}" != "200" ]; then
    echo "  ✗ 实例没起来，日志尾部:"
    tail -5 "$dir/run.log" | sed 's/^/      /'
    kill "$pid" 2>/dev/null
    return 1
  fi

  echo "  确认处于首启模式:"
  grep -c "first launch" "$dir/run.log" 2>/dev/null | sed 's/^/      日志里 first launch 出现 /;s/$/ 次/'

  # 走向导（就是网页引导安装那一步）
  echo "  → POST /control/install/configure"
  curl -s -m 40 -X POST "$BASE/control/install/configure" \
    -H 'Content-Type: application/json' \
    -d "{\"language\":\"zh-cn\",\"username\":\"admin\",\"password\":\"TestPass12345\",
         \"web\":{\"ip\":\"127.0.0.1\",\"port\":$WEB_PORT},
         \"dns\":{\"ip\":\"127.0.0.1\",\"port\":$DNS_PORT}}" \
    -o "$dir/wizard.out" -w "      http=%{http_code}\n"

  # 向导会重启 web，等它回来
  for i in $(seq 1 40); do
    sleep 1
    code=$(curl -s -o /dev/null -w '%{http_code}' -m 3 "$BASE/install.html" 2>/dev/null)
    [ "$code" = "200" ] && break
  done
  sleep 2

  echo "  === 向导完成后，不重启，直接看门户 ==="
  local portal
  portal=$(curl -s -o /dev/null -w '%{http_code}' -m 8 "$BASE/portal/" 2>/dev/null)
  printf "      /portal/                  %s\n" "$portal"

  # 门户路由注册后，日志里会有这一行
  if grep -q "user portal is enabled" "$dir/run.log" 2>/dev/null; then
    echo "      日志: user portal is enabled ✓"
  else
    echo "      日志: 没有 'user portal is enabled' ✗"
  fi

  # 管理员侧的下载 / 邮件测试（未认证是 401，路由不存在才是 404）
  local dl mt
  dl=$(curl -s -o /dev/null -w '%{http_code}' -m 8 "$BASE/control/portal/package" 2>/dev/null)
  mt=$(curl -s -o /dev/null -w '%{http_code}' -m 8 -X POST -H 'Content-Type: application/json' -d '{}' \
        "$BASE/control/portal/mail/test" 2>/dev/null)
  printf "      /control/portal/package   %s\n" "$dl"
  printf "      /control/portal/mail/test %s\n" "$mt"

  if [ "$portal" = "200" ]; then
    echo "  ✅ 结论: 向导后门户立即可用"
  else
    echo "  ❌ 结论: 向导后门户仍是 $portal"
  fi

  kill "$pid" 2>/dev/null
  wait "$pid" 2>/dev/null
  sleep 1
  echo
}

run_case "旧版 v1.0.38（预期失败）" /root/agold/aghub /root/agtest-old
run_case "新版 v1.0.39（预期修复）" /root/aghub-v1039 /root/agtest-new

echo "=============================================================="
echo "  清理测试目录"
rm -rf /root/agtest-old /root/agtest-new
echo "  已清理"
