<?php
/**
 * AGHub 门户 —— 前端页面 + API 反向代理（PHP 版）
 *
 * 浏览器只跟这个 PHP 说话（同源），PHP 再去调 AGHub。服务端到服务端那一跳
 * 没有浏览器参与，所以：
 *
 *   - AGHub 那边用 http 还是 https 都行，不需要证书
 *   - 不需要 CORS，不需要在管理端配「门户来源」
 *   - 前端页面本身用 http 还是 https 都行
 *   - 会话 cookie 由 PHP 按浏览器实际协议兜底改写，不会出现"登录成功、刷新掉线"
 *
 * 目录结构（把 AGHub 门户部署包里的 index.html、config.js 一起放进来）：
 *
 *   <站点目录>/portal/index.php     ← 本文件
 *   <站点目录>/portal/index.html    ← 从 AGHub 门户部署包拿
 *   <站点目录>/portal/config.js     ← apiBase 留空（同源）
 *   <站点目录>/portal/config.php    ← 只有这个要改：填 AGHub 地址
 *
 * 访问 https://你的域名/portal/ 即可。
 *
 * 兼容 PHP 7.4（不用 str_starts_with 等 PHP 8 才有的函数）。
 */

declare(strict_types=1);

// ----------------------------------------------------------------- 配置读取

$configFile = __DIR__ . '/config.php';
if (!is_file($configFile)) {
    http_response_code(500);
    header('Content-Type: text/plain; charset=utf-8');
    echo "缺少 config.php\n\n";
    echo "把同目录下的 config.sample.php 复制成 config.php，填上 AGHub 地址即可：\n";
    echo "    cp config.sample.php config.php\n";
    exit;
}

$CONFIG = require $configFile;

$AGHUB_URL = rtrim((string)($CONFIG['aghub_url'] ?? ''), '/');
$PREFIX    = '/' . trim((string)($CONFIG['prefix'] ?? 'portal'), '/');
$TIMEOUT   = (int)($CONFIG['timeout'] ?? 20);
$VERIFY_TLS = (bool)($CONFIG['verify_tls'] ?? true);

if ($AGHUB_URL === '') {
    http_response_code(500);
    header('Content-Type: text/plain; charset=utf-8');
    echo "config.php 里的 aghub_url 是空的，填上 AGHub 的地址，例如 http://36.133.104.222:3000\n";
    exit;
}

// ----------------------------------------------------------------- 工具函数

/** 浏览器那一侧是不是 https。反代后面靠 X-Forwarded-Proto 判断。 */
function browser_is_https(): bool
{
    if (!empty($_SERVER['HTTPS']) && strtolower((string)$_SERVER['HTTPS']) !== 'off') {
        return true;
    }
    if (isset($_SERVER['SERVER_PORT']) && (int)$_SERVER['SERVER_PORT'] === 443) {
        return true;
    }
    $proto = $_SERVER['HTTP_X_FORWARDED_PROTO'] ?? '';
    return strtolower(trim((string)$proto)) === 'https';
}

/** 当前请求的路径（不含查询串）。 */
function request_path(): string
{
    $uri  = (string)($_SERVER['REQUEST_URI'] ?? '/');
    $path = parse_url($uri, PHP_URL_PATH);
    return is_string($path) && $path !== '' ? $path : '/';
}

/**
 * 会话 cookie 的兜底改写。
 *
 * AGHub 靠 X-Forwarded-Proto 判断浏览器那一侧是不是 https，正常情况下这里
 * 什么都不用改。但如果两边的协议对不上——比如 PHP 到 AGHub 走的是 https、
 * 而浏览器这边是 http——AGHub 会下发 Secure，浏览器在明文下会直接丢掉它，
 * 表现就是"登录提示成功，一刷新又掉线"。所以按浏览器实际的协议兜一道。
 */
function fix_set_cookie(string $cookie, bool $browserHttps): string
{
    if ($browserHttps) {
        return $cookie;
    }

    // 明文 http 下 Secure 会让浏览器直接丢弃这个 cookie。
    $cookie = preg_replace('/;\s*Secure\b/i', '', $cookie);

    // SameSite=None 必须搭配 Secure，没有 Secure 时浏览器同样会丢。同源场景
    // 用 Lax 就够。
    $cookie = preg_replace('/;\s*SameSite=None\b/i', '; SameSite=Lax', $cookie);

    return $cookie;
}

// ----------------------------------------------------------------- 路由

$path = request_path();

// 1) 接口请求：转发给 AGHub。
if (strncmp($path, $PREFIX . '/api/', strlen($PREFIX) + 5) === 0) {
    proxy_api($path);
    exit;
}

// 2) 其余路径：返回前端页面。
serve_frontend($path);

// ----------------------------------------------------------------- 反代实现

function proxy_api(string $path): void
{
    global $AGHUB_URL, $TIMEOUT, $VERIFY_TLS;

    $target = $AGHUB_URL . $path;

    $query = (string)($_SERVER['QUERY_STRING'] ?? '');
    if ($query !== '') {
        $target .= '?' . $query;
    }

    $ch = curl_init($target);
    if ($ch === false) {
        fail('无法初始化 cURL 会话。请确认 PHP 装了 curl 扩展。');
    }

    // ---- 请求头：只带必要的几个。
    //
    // 刻意不转发 Origin：AGHub 看到 Origin 会按跨域处理，下发的 cookie 变成
    // SameSite=None（需要 Secure，明文下会被丢）。这里浏览器跟 PHP 是同源，
    // 不带 Origin 才是对的。
    $headers = array();
    foreach (array('CONTENT_TYPE' => 'Content-Type', 'HTTP_ACCEPT' => 'Accept') as $key => $name) {
        if (!empty($_SERVER[$key])) {
            $headers[] = $name . ': ' . $_SERVER[$key];
        }
    }
    if (!empty($_SERVER['HTTP_COOKIE'])) {
        $headers[] = 'Cookie: ' . $_SERVER['HTTP_COOKIE'];
    }

    // 让 AGHub 知道浏览器那一侧的真实协议。
    $scheme = browser_is_https() ? 'https' : 'http';
    $headers[] = 'X-Forwarded-Proto: ' . $scheme;
    if (!empty($_SERVER['REMOTE_ADDR'])) {
        $headers[] = 'X-Forwarded-For: ' . $_SERVER['REMOTE_ADDR'];
    }

    // ---- 请求体
    $body = file_get_contents('php://input');
    $method = strtoupper((string)($_SERVER['REQUEST_METHOD'] ?? 'GET'));

    $responseHeaders = array();

    curl_setopt_array($ch, array(
        CURLOPT_CUSTOMREQUEST  => $method,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_HTTPHEADER     => $headers,
        CURLOPT_TIMEOUT        => $TIMEOUT,
        CURLOPT_CONNECTTIMEOUT => min(10, $TIMEOUT),
        CURLOPT_FOLLOWLOCATION => false,
        CURLOPT_SSL_VERIFYPEER => $VERIFY_TLS,
        CURLOPT_SSL_VERIFYHOST => $VERIFY_TLS ? 2 : 0,
        CURLOPT_HEADERFUNCTION => function ($ch, string $line) use (&$responseHeaders) {
            $trimmed = trim($line);
            if ($trimmed !== '') {
                $responseHeaders[] = $trimmed;
            }
            return strlen($line);
        },
    ));

    if ($body !== false && $body !== '' && in_array($method, array('POST', 'PUT', 'PATCH', 'DELETE'), true)) {
        curl_setopt($ch, CURLOPT_POSTFIELDS, $body);
    }

    $responseBody = curl_exec($ch);
    $errNo        = curl_errno($ch);
    $errMsg       = curl_error($ch);
    $status       = (int)curl_getinfo($ch, CURLINFO_HTTP_CODE);
    curl_close($ch);

    if ($errNo !== 0 || $responseBody === false) {
        fail('连不上 AGHub（' . $AGHUB_URL . '）：' . $errMsg, 502);
    }

    // ---- 回传状态码与响应头
    http_response_code($status > 0 ? $status : 502);

    $browserHttps = browser_is_https();
    $seenCookie   = false;

    foreach ($responseHeaders as $line) {
        $lower = strtolower($line);

        // Content-Length 由 PHP 自己算，转发会导致截断或挂住。
        if (strpos($lower, 'content-length:') === 0) {
            continue;
        }
        // 长度交给 PHP；分块编码也要去掉。
        if (strpos($lower, 'transfer-encoding:') === 0) {
            continue;
        }
        if (strpos($lower, 'connection:') === 0) {
            continue;
        }

        if (strpos($lower, 'set-cookie:') === 0) {
            $seenCookie = true;
            header('Set-Cookie: ' . fix_set_cookie(substr($line, 11), $browserHttps), false);
            continue;
        }

        header($line, false);
    }

    // 后端没给 cookie 时不要凭空造一个，但要让 PHP 知道这里没有 cookie 可发。
    if (!$seenCookie) {
        // 什么都不做：header() 列表里本来就没有 Set-Cookie。
    }

    echo $responseBody;
}

function fail(string $message, int $status = 500): void
{
    http_response_code($status);
    header('Content-Type: application/json; charset=utf-8');
    echo json_encode(array(
        'error'   => $message,
        'hint'    => '检查 config.php 里的 aghub_url，以及在浏览器里直接打开该地址看通不通。',
    ), JSON_UNESCAPED_UNICODE);
    exit;
}

// ----------------------------------------------------------------- 前端

function serve_frontend(string $path): void
{
    global $PREFIX;

    // 只服务白名单文件，避免变成任意文件读取。
    $name = basename($path);
    if ($name === '' || $name === '/') {
        $name = 'index.html';
    }

    $allowed = array(
        'index.html' => 'text/html; charset=utf-8',
        'config.js'  => 'application/javascript; charset=utf-8',
    );

    if (!isset($allowed[$name])) {
        // 前端是单页，未知路径统一回首页。
        $name = 'index.html';
    }

    $file = __DIR__ . '/' . $name;
    if (!is_file($file)) {
        http_response_code(500);
        header('Content-Type: text/plain; charset=utf-8');
        echo "缺少前端文件 " . $name . "。\n\n";
        echo "从 AGHub 管理端的「门户」页面下载门户部署包，把里面的 index.html 和\n";
        echo "config.js 上传到本目录（" . __DIR__ . "）即可。\n";
        exit;
    }

    header('Content-Type: ' . $allowed[$name]);
    header('Cache-Control: no-cache');
    readfile($file);
}
