package portal

import (
	"archive/zip"
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"
)

//go:embed phpfiles
var phpFiles embed.FS

const (
	// PackageFileName is the name of the generated deployment package.
	PackageFileName = "aghub-portal.zip"

	// configFileName is the file the portal reads its settings from.  It is
	// generated, not shipped, because it carries the pairing token.
	configFileName = "config.php"

	// readmeFileName is the deployment note inside the package.
	readmeFileName = "部署说明.txt"

	// sampleFileName is the shipped sample configuration.  It is dropped from
	// the package: the generated config.php replaces it.
	sampleFileName = "config.sample.php"
)

// Package builds the deployment package of the portal.
//
// apiBase is the address of AGHub as the portal's PHP reaches it, and token is
// the pairing token.  Both are written straight into the generated config.php,
// so the administrator unzips the archive and is done -- there is nothing to
// fill in.
//
// The package is built from the portal embedded into the binary, so it needs
// no build step and always matches the running version.
func Package(apiBase, token string) (b []byte, err error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// The PHP portal goes in flat, so that unzipping into a site's document
	// root works as it is.
	err = fs.WalkDir(phpFiles, "phpfiles", func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}

		name := path.Base(p)
		if d.IsDir() || name == sampleFileName {
			return nil
		}

		data, rerr := fs.ReadFile(phpFiles, p)
		if rerr != nil {
			return rerr
		}

		return writeZipFile(zw, name, data)
	})
	if err != nil {
		_ = zw.Close()

		return nil, fmt.Errorf("portal: packing %q: %w", PackageFileName, err)
	}

	err = writeZipFile(zw, configFileName, configPHP(apiBase, token))
	if err != nil {
		_ = zw.Close()

		return nil, fmt.Errorf("portal: packing the config: %w", err)
	}

	err = writeZipFile(zw, readmeFileName, readme(apiBase))
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

// writeZipFile adds one file to the archive with a fixed timestamp, so that
// building the same package twice gives the same bytes.
func writeZipFile(zw *zip.Writer, name string, data []byte) (err error) {
	hdr := &zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: time.Unix(0, 0),
	}

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}

	_, err = w.Write(data)

	return err
}

// configPHP renders the portal configuration with the address and the token
// already in it.
func configPHP(apiBase, token string) (b []byte) {
	if apiBase == "" {
		// The PHP hop is server-to-server, so the address has to be one the
		// portal's host can reach.  An empty value would be a broken config,
		// so fall back to the usual local address.
		apiBase = "http://127.0.0.1:3000"
	}

	sb := &strings.Builder{}

	sb.WriteString("<?php\n")
	sb.WriteString("/**\n")
	sb.WriteString(" * AGHub 门户配置。由管理端生成，重新打包会覆盖这个文件。\n")
	sb.WriteString(" *\n")
	sb.WriteString(" * AGHUB_URL 是 PHP 在服务端访问 AGHub 用的地址，不是浏览器访问的地址。\n")
	sb.WriteString(" * 所以 http 或 https 都行，跟你的站点是不是 https 没有关系。\n")
	sb.WriteString(" * TOKEN 是这份部署包的对接令牌，已经填好，不用动。\n")
	sb.WriteString(" */\n\n")
	sb.WriteString("return array(\n")
	sb.WriteString("\n    // AGHub 的地址，带端口，不要结尾斜杠。\n")
	sb.WriteString("    'aghub_url' => " + quotePHP(apiBase) + ",\n")
	sb.WriteString("\n    // 对接令牌。PHP 每次请求都带在 X-Portal-Token 头上，用来确认调用\n")
	sb.WriteString("    // 来自你打包的这份门户。它不是账号凭据——用户仍然用自己的密码登录。\n")
	sb.WriteString("    // 万一这份包泄露，去管理端重新生成一个，旧包立刻失效。\n")
	sb.WriteString("    'token' => " + quotePHP(token) + ",\n")
	sb.WriteString("\n    // AGHub 用自签证书时改成 false。\n")
	sb.WriteString("    'verify_tls' => true,\n")
	sb.WriteString("\n    // 连 AGHub 的超时（秒）。\n")
	sb.WriteString("    'timeout' => 10,\n")
	sb.WriteString("\n    // 站点标题。\n")
	sb.WriteString("    'title' => 'DNS 服务',\n")
	sb.WriteString(");\n")

	return []byte(sb.String())
}

// quotePHP quotes s as a PHP single-quoted string literal.
func quotePHP(s string) (q string) {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)

	return "'" + s + "'"
}

// readme renders the deployment note.
func readme(apiBase string) (b []byte) {
	sb := &strings.Builder{}

	sb.WriteString("AGHub 门户 —— 部署说明\n")
	sb.WriteString("======================\n\n")
	sb.WriteString("这是一个用 PHP 写的门户：页面由 PHP 输出，浏览器只跟你的站点说话。\n")
	sb.WriteString("所有对 AGHub 的请求都是 PHP 在服务端发的，所以：\n\n")
	sb.WriteString("    · 没有跨域\n")
	sb.WriteString("    · 没有混合内容——你的站点是 https、AGHub 是 http 也没问题\n")
	sb.WriteString("    · AGHub 那边不需要证书\n")
	sb.WriteString("    · 不需要在 AGHub 里登记任何来源\n\n")
	sb.WriteString("三步\n")
	sb.WriteString("----\n\n")
	sb.WriteString("1. 把本目录所有文件传到站点目录，保持同级。宝塔里就是传到站点根目录，\n")
	sb.WriteString("   或者传到一个子目录（比如 /portal/），访问对应路径即可。\n\n")
	sb.WriteString("2. config.php 已经填好了 AGHUB_URL 和 TOKEN，一般不用改。\n")
	sb.WriteString("   只有 AGHub 不在本机时，才需要把 AGHUB_URL 改成它的地址：\n\n")
	sb.WriteString("       " + apiBaseForNote(apiBase) + "\n\n")
	sb.WriteString("3. 用浏览器打开站点。完成。\n\n")
	sb.WriteString("要求\n")
	sb.WriteString("----\n\n")
	sb.WriteString("PHP 7.4 或更高，需要 curl 扩展（宝塔默认都有）。\n")
	sb.WriteString("不需要数据库，不需要装任何东西。\n\n")
	sb.WriteString("出问题了\n")
	sb.WriteString("--------\n\n")
	sb.WriteString("页面顶部会直接显示原因，照着改就行：\n\n")
	sb.WriteString("    「连不上 AGHub」      AGHUB_URL 填错了，或者 AGHub 没在跑\n")
	sb.WriteString("    「对接令牌不对」      去 AGHub 管理端 → 门户 → 复制新的对接令牌，\n")
	sb.WriteString("                          填进 config.php 的 token\n")
	sb.WriteString("    「证书不受信任」      AGHub 用自签证书，把 config.php 里\n")
	sb.WriteString("                          verify_tls 改成 false\n\n")
	sb.WriteString("关于令牌\n")
	sb.WriteString("--------\n\n")
	sb.WriteString("令牌的作用是让 AGHub 确认调用来自你打包的这份门户。PHP 每次请求都把它\n")
	sb.WriteString("放在 X-Portal-Token 请求头上发给 AGHub。它只能调门户接口，不能当账号用\n")
	sb.WriteString("——用户仍然用自己的用户名和密码登录。\n\n")
	sb.WriteString("在管理端点「重新生成令牌」会让所有旧部署包失效，这是包泄露时的补救办法。\n")

	return []byte(sb.String())
}

// apiBaseForNote renders the API address for the deployment note.
func apiBaseForNote(apiBase string) (s string) {
	if apiBase == "" {
		return "（未填写，将使用 http://127.0.0.1:3000）"
	}

	return apiBase
}
