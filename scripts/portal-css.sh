#!/usr/bin/env bash
# 把 Tailwind 编译好并内联进门户单文件前端。
#
# 为什么不是直接引 CDN：实测 cdn.tailwindcss.com 只处理页面初始 HTML 里出现的
# 类。这个前端的 DOM 全部由 JS 渲染，动态插进去的类（grid-cols-2、text-[12px]
# 之类）在页面上完全没有样式——页面还是 200，只有浏览器里是白板。编译一次内联
# 进去，顺带不再依赖外网 CDN。
#
# 改完 index.html 里的类名之后跑一次这个脚本，然后重新构建 aghub 二进制
# （静态文件是 go:embed 进去的）。
#
# 用法: bash scripts/portal-css.sh
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
HTML="$ROOT/internal/portal/static/index.html"
INPUT="$ROOT/scripts/portal-tailwind.css"
CONF="$ROOT/scripts/portal-tailwind.config.js"

if [ ! -f "$HTML" ] || [ ! -f "$INPUT" ]; then
	echo "找不到输入文件：$HTML / $INPUT"
	exit 2
fi

# 找一个 tailwindcss：优先仓库自带的，其次 npx（会联网拉包）。
TW=""
for cand in "$ROOT/node_modules/.bin/tailwindcss" "$ROOT/client_v2/node_modules/.bin/tailwindcss"; do
	if [ -x "$cand" ]; then TW="$cand"; break; fi
done
if [ -z "$TW" ] && command -v npx >/dev/null 2>&1; then
	TW="npx --yes tailwindcss@3.4.17"
fi
if [ -z "$TW" ]; then
	echo "没有 tailwindcss：npm i -D tailwindcss@3 之后再跑，或把 PORTAL_TAILWIND 指过去。"
	exit 2
fi
if [ -n "${PORTAL_TAILWIND:-}" ]; then TW="$PORTAL_TAILWIND"; fi

OUT=$(mktemp)
# 必须在仓库根目录跑：配置里的 content 是相对「当前工作目录」解析的，从别处跑
# 就会扫不到类，编译出一份几乎空的 CSS —— 服务端照样 200，页面却是没样式的白板。
cd "$ROOT"
# shellcheck disable=SC2086
$TW -c "$CONF" -i "$INPUT" -o "$OUT" --minify 2>&1 | tail -3

SIZE=$(wc -c < "$OUT")
if [ "$SIZE" -lt 12000 ]; then
	echo "编译出的 CSS 只有 $SIZE 字节，明显没扫到类（正常 20KB 以上）。"
	echo "检查 $CONF 里的 content 路径和当前工作目录。"
	exit 1
fi

python3 - "$HTML" "$OUT" <<'PY'
import re, sys
html_path, css_path = sys.argv[1], sys.argv[2]
css = open(css_path, encoding="utf-8").read().strip()
html = open(html_path, encoding="utf-8").read()

start = "/* @portal-css-start */"
end = "/* @portal-css-end */"
if start not in html or end not in html:
    print(f"index.html 里找不到 {start} / {end} 标记")
    sys.exit(2)

before = html.split(start)[0]
after = html.split(end)[1]
html = before + start + "\n" + css + "\n" + end + after
open(html_path, "w", encoding="utf-8").write(html)
print(f"已内联 {len(css)} 字节 CSS 到 {html_path}")
PY

rm -f "$OUT"
echo "完成。接着重新构建二进制：go build -o <out> ."
