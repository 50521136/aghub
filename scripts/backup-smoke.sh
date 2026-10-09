#!/bin/bash
# 端到端验证 AGHub 的分档备份：跑真实二进制、建用户、等落盘、检查两个档位的文件。
set -u

WORK=/root/agh-backup-smoke
PORT=3001
DNS_PORT=5353
PASS='Smoke@Test123'

# 默认用本地构建；传参可指定任意 tar.gz（例如从 GitHub Release 下载的正式产物）。
TARBALL="${1:-/root/agh-src/dist-backupopt/aghub_1.0.36_linux_amd64.tar.gz}"

rm -rf "$WORK"; mkdir -p "$WORK"
cd "$WORK"
echo "=== 被测产物: $TARBALL ==="
tar -xzf "$TARBALL" ./aghub
chmod +x aghub

echo "=== 版本 ==="
./aghub --version 2>&1 | head -2

echo
echo "=== 启动（web :$PORT / dns :$DNS_PORT）==="
./aghub -w "$WORK" -s run --web-addr "127.0.0.1:$PORT" \
    >"$WORK/aghub.log" 2>&1 &
PID=$!
echo "  pid=$PID"

# 等 web 起来
for i in $(seq 1 30); do
    code=$(curl -s -o /dev/null -m 3 -w '%{http_code}' "http://127.0.0.1:$PORT/" 2>/dev/null)
    [ "$code" != "000" ] && { echo "  web 就绪 (第 $i 次探测, HTTP $code)"; break; }
    sleep 1
done

echo
echo "=== 走完安装向导 ==="
curl -s -m 30 -o /dev/null -w '  configure HTTP=%{http_code}\n' \
    -X POST "http://127.0.0.1:$PORT/control/install/configure" \
    -H 'Content-Type: application/json' \
    -d "{\"web\":{\"ip\":\"127.0.0.1\",\"port\":$PORT},\"dns\":{\"ip\":\"127.0.0.1\",\"port\":$DNS_PORT},\"username\":\"admin\",\"password\":\"$PASS\"}"

sleep 6

echo
echo "=== 登录 ==="
CJ="$WORK/cj"; rm -f "$CJ"
curl -s -m 20 -c "$CJ" -o /dev/null -w '  login HTTP=%{http_code}\n' \
    -X POST "http://127.0.0.1:$PORT/control/login" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"admin\",\"password\":\"$PASS\"}"

echo
echo "=== 建 3 个用户 ==="
i=0
for n in alice bob carol; do
    i=$((i + 1))
    curl -s -m 20 -b "$CJ" -o /dev/null -w "  add $n HTTP=%{http_code}\n" \
        -X POST "http://127.0.0.1:$PORT/control/users/add" \
        -H 'Content-Type: application/json' \
        -d "{\"name\":\"$n\",\"ids\":[\"10.9.9.$i\"],\"enabled\":true,\"request_limit\":100000}"
done

echo
echo "=== 等后台落盘（30s 周期 + 余量）==="
for i in $(seq 1 45); do
    sleep 2
    if [ -d "$WORK/data/backup" ] && [ -n "$(ls -A "$WORK/data/backup" 2>/dev/null)" ]; then
        echo "  第 $((i*2))s 出现备份"
        break
    fi
done

echo
echo "=== 备份目录内容 ==="
ls -la "$WORK/data/backup" 2>/dev/null || echo "  ✗ 没有备份目录"

echo
echo "=== 按档位分类 ==="
python3 - "$WORK" <<'PY'
import os, re, sys, json, glob
work = sys.argv[1]
bdir = os.path.join(work, 'data', 'backup')
files = sorted(os.listdir(bdir)) if os.path.isdir(bdir) else []
tiers = {}
for f in files:
    label = f[len('users.json.') : -len('.bak')] if f.startswith('users.json.') and f.endswith('.bak') else None
    if label is None:
        continue
    if label.endswith('-before-restore'):
        t = 'restore'
    elif 'T' in label:
        t = 'hourly'
    else:
        t = 'daily'
    tiers.setdefault(t, []).append(f)

print(f"  文件总数: {len(files)}")
for t in ('hourly', 'daily', 'restore'):
    got = tiers.get(t, [])
    mark = '✓' if got else '✗'
    print(f"  {mark} {t:8s} {len(got)} 个: {got}")

# 备份内容必须是完整状态
for t in ('hourly', 'daily'):
    for f in tiers.get(t, []):
        with open(os.path.join(bdir, f), encoding='utf-8') as fh:
            st = json.load(fh)
        print(f"    {f}: version={st.get('version')} users={len(st.get('users') or [])} "
              f"names={[u['name'] for u in (st.get('users') or [])]}")

print()
ok = bool(tiers.get('hourly')) and bool(tiers.get('daily'))
print("  结果:", "PASS —— 小时档与天档都已生成" if ok else "FAIL —— 档位不全")
PY

echo
echo "=== UI 用的备份接口 /control/users/backups ==="
curl -s -m 20 -b "$CJ" "http://127.0.0.1:$PORT/control/users/backups" | python3 -c "
import json, sys
d = json.load(sys.stdin)
bs = d.get('backups') or []
print(f'  接口返回 {len(bs)} 份备份（UI 列表就渲染这个）:')
for b in bs:
    print(f\"    {b['name']}  users={b['users']}  size={b['size']}\")
" 2>&1 | head -10

echo
echo "=== 服务日志尾部（看有没有备份报错）==="
grep -iE "backup|error|warn" "$WORK/aghub.log" 2>/dev/null | tail -8 || echo "  (无相关日志)"

kill $PID 2>/dev/null
wait $PID 2>/dev/null
echo
echo "=== 已停止 ==="
