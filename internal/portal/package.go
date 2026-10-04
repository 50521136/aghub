package portal

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"strings"
)

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
func readme(apiBase string, origins []string) (b []byte) {
	var sb strings.Builder

	sb.WriteString("AGHub 门户前端部署包\n")
	sb.WriteString("====================\n\n")
	sb.WriteString("这个目录就是完整的门户前端，纯静态文件，不需要 Node、PHP 或任何后端。\n\n")

	sb.WriteString("部署步骤\n--------\n\n")
	sb.WriteString("1. 把这里的文件原样上传到你的网站目录（根目录或任意子目录都可以）。\n\n")

	if apiBase == "" {
		sb.WriteString("2. config.js 里的 apiBase 是空的，表示前端与 API 同源。\n")
		sb.WriteString("   如果你要把它部署到别的域名，请回到 AGHub 管理端的「门户部署」\n")
		sb.WriteString("   里填上 API 地址，然后重新下载这个包。\n\n")
	} else {
		sb.WriteString("2. config.js 里的 apiBase 已经指向：\n")
		sb.WriteString("     " + apiBase + "\n\n")
	}

	if len(origins) == 0 {
		sb.WriteString("3. 注意：AGHub 里还没有配置「门户来源」，所以 API 只接受来自\n")
		sb.WriteString("   AGHub 自带页面的请求。请把下面这个页面的来源填进\n")
		sb.WriteString("   管理端「设置 → 门户来源」，否则浏览器会拦下跨域请求：\n")
		sb.WriteString("     https://你的前端域名\n\n")
	} else {
		sb.WriteString("3. AGHub 里配置的「门户来源」是：\n")
		for _, o := range origins {
			sb.WriteString("     " + o + "\n")
		}
		sb.WriteString("   这个页面必须部署在其中一个来源上，否则浏览器会拦下跨域请求。\n\n")
	}

	sb.WriteString("4. 必须用 HTTPS 提供这个页面，API 也必须是 HTTPS。跨域登录用的\n")
	sb.WriteString("   cookie 带 SameSite=None，浏览器在非 HTTPS 下会直接丢弃它；\n")
	sb.WriteString("   而 HTTPS 页面又无法请求明文 HTTP 的接口（混合内容会被拦掉）。\n")
	sb.WriteString("   两者都表现为\"登录没反应\"，前端页面会显示一条说明告诉你具体是哪一种。\n\n")

	sb.WriteString("   AGHub 自带 HTTPS，不需要额外的反向代理：\n")
	sb.WriteString("     aghub -w /opt/aghub --web-addr 0.0.0.0:3000 \\\n")
	sb.WriteString("       --web-tls-cert /path/fullchain.pem \\\n")
	sb.WriteString("       --web-tls-key  /path/privkey.pem\n")
	sb.WriteString("   证书和私钥必须同时给，只给一个 AGHub 会拒绝启动。\n")
	sb.WriteString("   用 nginx 反代也可以，但必须设置 X-Forwarded-Proto: https，\n")
	sb.WriteString("   否则 AGHub 会以为自己在明文 HTTP 上，cookie 同样会被丢弃。\n\n")

	sb.WriteString("5. 页面本身没有任何配置项，改地址请改 config.js 里的 apiBase。\n")

	return []byte(sb.String())
}
