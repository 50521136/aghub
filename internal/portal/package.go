package portal

import (
	"archive/zip"
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed phpfiles
var phpFiles embed.FS

// phpDir is where the PHP back-end lands inside the package.
const phpDir = "php/"

const (
	// PackageFileName is the name of the generated deployment package.
	PackageFileName = "aghub-portal.zip"

	// configFileName is the file the page reads its API address from.
	configFileName = "config.js"

	// readmeFileName is the deployment note inside the package.
	readmeFileName = "部署说明.txt"
)

// Package builds the deployment package of the portal front-end.  apiBase is
// the address of the API as the browser reaches it and is written into
// config.js; origins are the origins the API accepts and only go into the
// deployment note, so that the administrator can check them against the
// settings.
//
// The package is built from the front-end embedded into the binary, so it
// needs no build step and always matches the running version.
func Package(apiBase string, origins []string) (b []byte, err error) {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("portal: getting the static subdirectory: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// WalkDir visits the entries in lexical order, so the archive is
	// reproducible and the note ends up last.
	err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		data, rerr := fs.ReadFile(sub, path)
		if rerr != nil {
			return rerr
		}

		if path == configFileName {
			data = configJS(apiBase)
		}

		return writeZipFile(zw, path, data)
	})
	if err != nil {
		_ = zw.Close()

		return nil, fmt.Errorf("portal: packing %q: %w", PackageFileName, err)
	}

	// The PHP back-end goes in as well.  A site that has PHP but no reverse
	// proxy can then serve the portal over http or https without touching the
	// AGHub host at all: the browser only ever talks to PHP, and the PHP hop
	// is not subject to the browser rules that break a plain HTTP API.
	err = addPHPFiles(zw)
	if err != nil {
		_ = zw.Close()

		return nil, fmt.Errorf("portal: packing the php back-end: %w", err)
	}

	err = writeZipFile(zw, readmeFileName, readme(apiBase, origins))
	if err != nil {
		_ = zw.Close()

		return nil, fmt.Errorf("portal: packing the readme: %w", err)
	}

	err = zw.Close()
	if err != nil {
		return nil, fmt.Errorf("portal: closing the package: %w", err)
	}

	return buf.Bytes(), nil
}

// writeZipFile adds a single file to the archive.
// addPHPFiles puts the PHP back-end into the package under php/.
func addPHPFiles(zw *zip.Writer) (err error) {
	sub, err := fs.Sub(phpFiles, "phpfiles")
	if err != nil {
		return fmt.Errorf("getting the phpfiles subdirectory: %w", err)
	}

	return fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		data, rerr := fs.ReadFile(sub, path)
		if rerr != nil {
			return rerr
		}

		return writeZipFile(zw, phpDir+path, data)
	})
}

func writeZipFile(zw *zip.Writer, name string, data []byte) (err error) {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}

	_, err = w.Write(data)

	return err
}

// configJS renders the configuration file of the front-end.
func configJS(apiBase string) (b []byte) {
	// The address is written as a JSON string, which is also a valid
	// JavaScript one.  Quoting it keeps a stray quote in the address from
	// breaking the page.
	return []byte(fmt.Sprintf(
		"// 由 AGHub 管理端生成，重新打包会覆盖这个文件。\n"+
			"window.AGHUB_PORTAL_CONFIG = {\n"+
			"  // 留空表示与页面同源。\n"+
			"  apiBase: %s,\n"+
			"};\n",
		quoteJS(apiBase),
	))
}

// quoteJS quotes s as a JavaScript string literal.
func quoteJS(s string) (q string) {
	var b strings.Builder
	b.WriteByte('"')

	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}

	b.WriteByte('"')

	return b.String()
}

// readme renders the deployment note.
//
// The note leads with the same-origin reverse proxy, because that is the setup
// most deployments already have: a server-side hop is not subject to the
// browser rules that make a cross-origin portal fail, so the AGHub host can
// keep serving plain HTTP.  Direct browser access to the API is documented
// second, as the alternative that does need TLS.
func readme(apiBase string, origins []string) (b []byte) {
	var sb strings.Builder

	sb.WriteString("AGHub 门户前端部署包\n")
	sb.WriteString("====================\n\n")
	sb.WriteString("这个目录是完整的门户前端（纯静态），另外带一个可选的 PHP 后端。\n\n")

	sb.WriteString("先读这一段：浏览器发出的明文 HTTP 请求会被拦掉\n")
	sb.WriteString("------------------------------------------------\n\n")
	sb.WriteString("页面用 https 提供、而接口是 http 时，浏览器会按「混合内容」直接拦掉\n")
	sb.WriteString("这个请求；跨域时还多一道：会话 cookie 带 SameSite=None，明文 HTTP 下\n")
	sb.WriteString("会被静默丢弃，表现是「登录成功、刷新掉线」。\n\n")
	sb.WriteString("这两条都只针对**浏览器发出**的请求。服务端到服务端的明文 HTTP 不受\n")
	sb.WriteString("任何影响，所以只要让浏览器只跟自己同源的后端说话，国内的 AGHub\n")
	sb.WriteString("可以继续跑明文 HTTP，不用动。\n\n")

	sb.WriteString("推荐：同源反代（AGHub 不用改）\n")
	sb.WriteString("------------------------------\n\n")
	sb.WriteString("在前端服务器上把 /portal/ 反代到 AGHub：\n\n")
	sb.WriteString("    location /portal/ {\n")
	sb.WriteString("        proxy_pass http://<AGHub 地址>:3000;\n")
	sb.WriteString("        proxy_set_header Host              $host;\n")
	sb.WriteString("        proxy_set_header X-Real-IP         $remote_addr;\n")
	sb.WriteString("        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;\n")
	sb.WriteString("        proxy_set_header X-Forwarded-Proto https;\n")
	sb.WriteString("    }\n\n")
	sb.WriteString("X-Forwarded-Proto 这一行不能省：AGHub 靠它判断浏览器那一侧是 https，\n")
	sb.WriteString("少了它下发的 cookie 不带 Secure。\n\n")

	sb.WriteString("第二种：用包里的 PHP（站点有 PHP、没有反向代理）\n")
	sb.WriteString("--------------------------------------------------\n\n")
	sb.WriteString("把 php/ 里的三个文件和本目录的 index.html、config.js 放在站点的\n")
	sb.WriteString("portal/ 目录下，改一下 php/config.sample.php 里的 aghub_url，\n")
	sb.WriteString("再复制成 config.php：\n\n")
	sb.WriteString("    <站点目录>/portal/index.php        php/index.php\n")
	sb.WriteString("    <站点目录>/portal/config.php       php/config.sample.php 复制而来\n")
	sb.WriteString("    <站点目录>/portal/index.html       本目录的 index.html\n")
	sb.WriteString("    <站点目录>/portal/config.js        本目录的 config.js（apiBase 留空）\n\n")
	sb.WriteString("浏览器只跟 PHP 说话（同源），PHP 再去调 AGHub。这一跳是服务端到\n")
	sb.WriteString("服务端的，不受浏览器策略约束，所以 AGHub 那边 http 还是 https 都行，\n")
	sb.WriteString("也不用配「门户来源」。nginx 和 Apache 的配置见 php/README.md。\n\n")

	sb.WriteString("备选：让浏览器直接访问 AGHub\n")
	sb.WriteString("------------------------------\n\n")
	sb.WriteString("这时 API 必须是 HTTPS，否则上面两种失败必然出现。AGHub 自带 HTTPS，\n")
	sb.WriteString("不需要额外的反向代理：\n\n")
	sb.WriteString("    aghub -w /opt/aghub --web-addr 0.0.0.0:3000 \\\n")
	sb.WriteString("      --web-tls-cert /path/fullchain.pem \\\n")
	sb.WriteString("      --web-tls-key  /path/privkey.pem\n\n")
	sb.WriteString("证书和私钥必须同时给，只给一个 AGHub 会拒绝启动。\n\n")

	sb.WriteString("本包的配置\n")
	sb.WriteString("----------\n\n")

	if apiBase == "" {
		sb.WriteString("config.js 里的 apiBase 是空的，表示前端与 API 同源 —— 这正是上面\n")
		sb.WriteString("推荐的用法，配合反代即可。只有在你选择「浏览器直接访问 AGHub」时\n")
		sb.WriteString("才需要回到管理端的「门户部署」填上 API 地址并重新下载这个包。\n\n")
	} else {
		sb.WriteString("config.js 里的 apiBase 已经指向：\n")
		sb.WriteString("    " + apiBase + "\n")
		sb.WriteString("这属于「浏览器直接访问 AGHub」，该地址必须是 https，且下面的来源\n")
		sb.WriteString("必须匹配。\n\n")
	}

	if len(origins) == 0 {
		sb.WriteString("「门户来源」没有配置。同源部署（反代）不需要它；如果你要让浏览器\n")
		sb.WriteString("跨域直连 AGHub，请把本页面的来源填进管理端「设置 → 门户来源」：\n")
		sb.WriteString("    https://你的前端域名\n\n")
	} else {
		sb.WriteString("管理端配置的「门户来源」是：\n")
		for _, o := range origins {
			sb.WriteString("    " + o + "\n")
		}
		sb.WriteString("跨域直连时页面必须部署在其中一个来源上，否则浏览器会拦下请求。\n")
		sb.WriteString("同源部署（反代）用不到这个配置。\n\n")
	}

	sb.WriteString("页面本身没有任何配置项，改地址请改 config.js 里的 apiBase。\n")

	return []byte(sb.String())
}
