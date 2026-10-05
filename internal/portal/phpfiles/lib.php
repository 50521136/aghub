<?php
/**
 * AGHub 门户 —— 公共库。
 *
 * 页面由 PHP 渲染，浏览器只跟本站说话。所有对 AGHub 的调用都发生在这里，
 * 服务端到服务端，所以不存在跨域，也不存在混合内容。
 */

declare(strict_types=1);

require_once __DIR__ . '/config.php';

/** 配置在 include 时被读进来一次。 */
function cfg(string $key, $default = null)
{
    static $conf = null;
    if ($conf === null) {
        $conf = require __DIR__ . '/config.php';
        if (!is_array($conf)) {
            $conf = array();
        }
    }

    return array_key_exists($key, $conf) ? $conf[$key] : $default;
}

// ---------------------------------------------------------------- AGHub 客户端

/**
 * aghub 调一次 AGHub 的门户接口，返回 array('ok' => bool, 'status' => int,
 * 'error' => string, 'data' => array)。
 *
 * 每次都带上对接令牌 —— 它就是让这份部署包能调接口的凭据。会话令牌（用户登录
 * 之后拿到的那个）放在 Authorization 头上传。
 */
function aghub(string $method, string $path, ?array $body = null, string $session = ''): array
{
    $url = rtrim((string) cfg('aghub_url', ''), '/') . $path;

    $headers = array(
        'Accept: application/json',
        'X-Portal-Token: ' . (string) cfg('token', ''),
    );

    if ($session !== '') {
        $headers[] = 'Authorization: Bearer ' . $session;
    }

    $opts = array(
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT => (int) cfg('timeout', 10),
        CURLOPT_CONNECTTIMEOUT => 5,
        CURLOPT_CUSTOMREQUEST => $method,
        CURLOPT_HTTPHEADER => $headers,
        CURLOPT_SSL_VERIFYPEER => (bool) cfg('verify_tls', true),
        CURLOPT_SSL_VERIFYHOST => cfg('verify_tls', true) ? 2 : 0,
    );

    if ($body !== null) {
        $headers[] = 'Content-Type: application/json';
        $opts[CURLOPT_HTTPHEADER] = $headers;
        $opts[CURLOPT_POSTFIELDS] = json_encode($body, JSON_UNESCAPED_UNICODE);
    }

    $ch = curl_init($url);
    curl_setopt_array($ch, $opts);
    $raw = curl_exec($ch);
    $code = (int) curl_getinfo($ch, CURLINFO_HTTP_CODE);
    $err = curl_error($ch);
    curl_close($ch);

    if ($raw === false) {
        return array(
            'ok' => false,
            'status' => 0,
            'error' => '连不上 AGHub（' . $url . '）：' . ($err !== '' ? $err : '未知错误'),
            'data' => array(),
        );
    }

    $data = json_decode((string) $raw, true);
    if (!is_array($data)) {
        $data = array();
    }

    $ok = $code >= 200 && $code < 300;

    return array(
        'ok' => $ok,
        'status' => $code,
        'error' => $ok ? '' : aghub_error_message($code, $data),
        'data' => $data,
    );
}

/** aghub_error_message 把 AGHub 返回的错误变成一句人话。 */
function aghub_error_message(int $code, array $data): string
{
    if (isset($data['error']) && is_string($data['error']) && $data['error'] !== '') {
        return $data['error'];
    }

    if ($code === 401) {
        return '登录状态已失效，请重新登录。';
    }

    if ($code === 403) {
        return '对接令牌不对，或者 AGHub 那边已经重新生成过令牌了。'
            . '去管理端复制新的，填进 config.php。';
    }

    if ($code === 0) {
        return '连不上 AGHub。';
    }

    return 'AGHub 返回了 HTTP ' . $code . '。';
}

// ------------------------------------------------------------------- 会话

/** session_boot 起一个属于本门户的会话，跟 AGHub 的会话互不影响。 */
function session_boot(): void
{
    if (session_status() === PHP_SESSION_ACTIVE) {
        return;
    }

    session_name('aghub_portal_php');
    session_set_cookie_params(array(
        'lifetime' => 0,
        'path' => '/',
        'httponly' => true,
        // 站点是 https 时带上 Secure；http 站点会自动省略，否则 cookie
        // 会被浏览器丢掉。
        'secure' => is_https(),
        'samesite' => 'Lax',
    ));
    session_start();
}

/** is_https 判断浏览器这一跳是不是 https（宝塔反代时看 X-Forwarded-Proto）。 */
function is_https(): bool
{
    if (!empty($_SERVER['HTTPS']) && strtolower((string) $_SERVER['HTTPS']) !== 'off') {
        return true;
    }

    if (isset($_SERVER['HTTP_X_FORWARDED_PROTO'])
        && strtolower((string) $_SERVER['HTTP_X_FORWARDED_PROTO']) === 'https') {
        return true;
    }

    return isset($_SERVER['SERVER_PORT']) && (int) $_SERVER['SERVER_PORT'] === 443;
}

/** session_token 取当前登录用户在 AGHub 那边的会话令牌。 */
function session_token(): string
{
    return isset($_SESSION['aghub_token']) ? (string) $_SESSION['aghub_token'] : '';
}

/** session_forget 退出登录。 */
function session_forget(): void
{
    unset($_SESSION['aghub_token'], $_SESSION['aghub_user']);
}

/** csrf_token 取（必要时生成）本会话的 CSRF 令牌。 */
function csrf_token(): string
{
    if (empty($_SESSION['csrf'])) {
        $_SESSION['csrf'] = bin2hex(random_bytes(16));
    }

    return (string) $_SESSION['csrf'];
}

/** csrf_ok 校验表单带回来的 CSRF 令牌。 */
function csrf_ok(): bool
{
    $sent = isset($_POST['csrf']) ? (string) $_POST['csrf'] : '';

    return $sent !== '' && !empty($_SESSION['csrf']) && hash_equals((string) $_SESSION['csrf'], $sent);
}

// ------------------------------------------------------------------- 显示

/** h 转义，所有输出都走它。 */
function h($v): string
{
    return htmlspecialchars((string) $v, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
}

/** bytes_h 把字节数变成人话。 */
function bytes_h(int $n): string
{
    if ($n < 1000) {
        return (string) $n;
    }

    $units = array('K', 'M', 'G', 'T');
    $v = (float) $n;
    foreach ($units as $u) {
        $v /= 1000.0;
        if ($v < 1000.0) {
            return round($v, 1) . $u;
        }
    }

    return round($v, 1) . 'P';
}

/** num_h 给数字加千分位。 */
function num_h(int $n): string
{
    return number_format($n);
}

/** period_h 把配额周期变成人话。 */
function period_h(string $p): string
{
    $map = array(
        'day' => '每天',
        'week' => '每周',
        'month' => '每月',
        'year' => '每年',
    );

    return isset($map[$p]) ? $map[$p] : $p;
}

/** status_h 把账号状态变成人话。 */
function status_h(string $s): string
{
    $map = array(
        'active' => '正常',
        'expired' => '已过期',
        'exhausted' => '配额用尽',
        'disabled' => '已停用',
    );

    return isset($map[$s]) ? $map[$s] : $s;
}

/** remaining_h 把剩余秒数变成人话。 */
function remaining_h(int $sec): string
{
    if ($sec < 0) {
        return '不限';
    }

    if ($sec === 0) {
        return '已到期';
    }

    $d = intdiv($sec, 86400);
    if ($d >= 1) {
        return $d . ' 天';
    }

    $hr = intdiv($sec, 3600);
    if ($hr >= 1) {
        return $hr . ' 小时';
    }

    return max(1, intdiv($sec, 60)) . ' 分钟';
}

/** when_h 时间戳变人话。 */
function when_h(int $ts): string
{
    if ($ts <= 0) {
        return '—';
    }

    return date('Y-m-d H:i', $ts);
}

/** since_h 相对时间。 */
function since_h(int $ts): string
{
    if ($ts <= 0) {
        return '从未';
    }

    $d = time() - $ts;
    if ($d < 60) {
        return '刚刚';
    }

    if ($d < 3600) {
        return intdiv($d, 60) . ' 分钟前';
    }

    if ($d < 86400) {
        return intdiv($d, 3600) . ' 小时前';
    }

    return intdiv($d, 86400) . ' 天前';
}

/** pct 算百分比，夹在 0..100。 */
function pct(int $used, int $limit): int
{
    if ($limit <= 0) {
        return 0;
    }

    $p = (int) round($used * 100 / $limit);

    return max(0, min(100, $p));
}

/** endpoint_h 从域名推出一个接入地址。 */
function dot_host(string $id, string $domain): string
{
    return $domain === '' ? '' : $id . '.' . $domain;
}

/** doh_url 拼出 DoH 地址。 */
function doh_url(string $id, string $domain): string
{
    $host = dot_host($id, $domain);

    return $host === '' ? '' : 'https://' . $host . '/dns-query';
}

/** page_url 拼本站地址。 */
function page_url(string $page = '', array $params = array()): string
{
    $q = array();
    if ($page !== '') {
        $q['p'] = $page;
    }

    foreach ($params as $k => $v) {
        $q[$k] = $v;
    }

    $base = strtok((string) $_SERVER['REQUEST_URI'], '?');
    if ($base === false || $base === '') {
        $base = '/';
    }

    // REQUEST_URI 可能带着脚本名，去掉 ?query 之后原样用。
    if (substr($base, -1) !== '/') {
        $base = dirname($base);
        if ($base === '.' || $base === '\\' || $base === '') {
            $base = '/';
        } else {
            $base .= '/';
        }
    }

    return $base . ($q === array() ? '' : '?' . http_build_query($q));
}

/** redirect 跳转并结束。 */
function redirect(string $url): void
{
    header('Location: ' . $url);
    exit;
}
