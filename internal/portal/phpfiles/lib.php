<?php
/**
 * AGHub 门户 —— 公共库。
 *
 * 页面由 PHP 渲染，浏览器只跟本站说话。所有对 AGHub 的调用都发生在这里，
 * 服务端到服务端，所以不存在跨域，也不存在混合内容。
 */

declare(strict_types=1);

require_once __DIR__ . '/config.php';

/** cfg 读配置，只读一次。 */
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
 */
function aghub(string $method, string $path, ?array $body = null, string $session = ''): array
{
    $url = rtrim((string) cfg('aghub_url', ''), '/') . $path;

    // 门户是从服务器这边去调 AGHub 的，AGHub 看到的地址是这台 Web 服务器。把
    // 访客真实的地址和 UA 一起转发过去，面板里的「连接情况」才不会写成服务器自己。
    $headers = array(
        'Accept: application/json',
        'X-Portal-Token: ' . (string) cfg('token', ''),
        'X-Portal-Client-IP: ' . client_ip(),
        'X-Portal-Client-UA: ' . visitor_ua(),
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
        // AGHub 的错误响应是纯文本，不是 JSON，所以正文在 $data 里拿不到。
        // 同一个状态码有多个原因的接口（注册：邮箱格式、验证码、重名）只能靠
        // 它分辨，否则用户看到的永远是「AGHub 返回了 HTTP 400」。
        'detail' => $ok ? '' : trim((string) $raw),
        'data' => $data,
    );
}

/**
 * aghub_multi 并发调多个门户接口，返回 array(键 => 和 aghub() 一样的结构)。
 *
 * 逐个 curl 的话，用户要等三次往返相加；并发只要等最慢的那一个。门户每开一页
 * 要问 AGHub 三四个接口，本地回环看不出来，跨机房就很明显 —— 站点在宝塔、
 * AGHub 在西安这种部署，每次往返按 50ms 算，串行就是 150ms 白等。
 *
 * $reqs 形如 array(键 => array($method, $path, $body, $session))，后两项可省。
 */
function aghub_multi(array $reqs): array
{
    $out = array();
    if ($reqs === array()) {
        return $out;
    }

    $base = rtrim((string) cfg('aghub_url', ''), '/');
    $timeout = (int) cfg('timeout', 10);
    $verify = (bool) cfg('verify_tls', true);

    $mh = curl_multi_init();
    $handles = array();

    foreach ($reqs as $key => $r) {
        $method = (string) $r[0];
        $path = (string) $r[1];
        $body = isset($r[2]) ? $r[2] : null;
        $session = isset($r[3]) ? (string) $r[3] : '';

        $headers = array(
            'Accept: application/json',
            'X-Portal-Token: ' . (string) cfg('token', ''),
            'X-Portal-Client-IP: ' . client_ip(),
            'X-Portal-Client-UA: ' . visitor_ua(),
        );
        if ($session !== '') {
            $headers[] = 'Authorization: Bearer ' . $session;
        }

        $opts = array(
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_TIMEOUT => $timeout,
            CURLOPT_CONNECTTIMEOUT => 5,
            CURLOPT_CUSTOMREQUEST => $method,
            CURLOPT_HTTPHEADER => $headers,
            CURLOPT_SSL_VERIFYPEER => $verify,
            CURLOPT_SSL_VERIFYHOST => $verify ? 2 : 0,
        );

        if ($body !== null) {
            $headers[] = 'Content-Type: application/json';
            $opts[CURLOPT_HTTPHEADER] = $headers;
            $opts[CURLOPT_POSTFIELDS] = json_encode($body, JSON_UNESCAPED_UNICODE);
        }

        $ch = curl_init($base . $path);
        curl_setopt_array($ch, $opts);
        curl_multi_add_handle($mh, $ch);
        $handles[$key] = $ch;
    }

    $running = null;
    do {
        $status = curl_multi_exec($mh, $running);
        if ($running > 0) {
            curl_multi_select($mh, 1.0);
        }
    } while ($running > 0 && $status === CURLM_OK);

    foreach ($handles as $key => $ch) {
        $raw = curl_multi_getcontent($ch);
        $code = (int) curl_getinfo($ch, CURLINFO_HTTP_CODE);
        $err = curl_error($ch);

        if ($raw === false || $raw === null || $raw === '') {
            $out[$key] = array(
                'ok' => false,
                'status' => 0,
                'error' => '连不上 AGHub（' . $base . '）：' . ($err !== '' ? $err : '未知错误'),
                'data' => array(),
            );
        } else {
            $data = json_decode((string) $raw, true);
            if (!is_array($data)) {
                $data = array();
            }
            $ok = $code >= 200 && $code < 300;
            $out[$key] = array(
                'ok' => $ok,
                'status' => $code,
                'error' => $ok ? '' : aghub_error_message($code, $data),
                'data' => $data,
            );
        }

        curl_multi_remove_handle($mh, $ch);
        curl_close($ch);
    }

    curl_multi_close($mh);

    return $out;
}

/** cache_item 取缓存里那条记录，没过期就返回，否则返回 null。 */
function cache_item(string $key, int $ttl)
{
    if (!isset($_SESSION['cache'][$key])) {
        return null;
    }

    $item = $_SESSION['cache'][$key];
    if (!is_array($item) || !isset($item['at'])) {
        return null;
    }

    if (time() - (int) $item['at'] > $ttl) {
        unset($_SESSION['cache'][$key]);

        return null;
    }

    return $item;
}

/**
 * fetch_all 把一批接口一次取回来：命中缓存的直接用，没命中的并发去拿。
 *
 * $want 形如 array(键 => array($req, $ttl))，$req 是 aghub_multi 要的形状。
 * 返回 array(键 => array('v','at','fresh'))。
 */
function fetch_all(array $want): array
{
    $out = array();
    $miss = array();
    $ttls = array();

    foreach ($want as $key => $pair) {
        $ttl = (int) $pair[1];
        $ttls[$key] = $ttl;

        $item = cache_item($key, $ttl);
        if ($item !== null) {
            $out[$key] = array('v' => $item['v'], 'at' => (int) $item['at'], 'fresh' => false);
        } else {
            $miss[$key] = $pair[0];
        }
    }

    if ($miss !== array()) {
        $got = aghub_multi($miss);
        foreach ($got as $key => $v) {
            cache_set($key, $v, $ttls[$key]);
            $out[$key] = array('v' => $v, 'at' => time(), 'fresh' => true);
        }
    }

    return $out;
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
        return '对接令牌不对，或者 AGHub 那边已经重新生成过令牌了。去管理端复制新的，填进 config.php。';
    }

    if ($code === 0) {
        return '连不上 AGHub。';
    }

    return 'AGHub 返回了 HTTP ' . $code . '。';
}

// --------------------------------------------------------------------- 缓存

/**
 * cache_get 取缓存值，过期或不存在时返回 $default。
 *
 * 日志这种翻来覆去看同一页的东西没必要每次都回源问 AGHub。缓存放在会话里，
 * 不落盘、不共享，所以不会串号。
 */
function cache_get(string $key, $default = null)
{
    if (!isset($_SESSION['cache'][$key])) {
        return $default;
    }

    $item = $_SESSION['cache'][$key];
    if (!is_array($item) || !isset($item['at'], $item['ttl'])) {
        return $default;
    }

    if (time() - (int) $item['at'] > (int) $item['ttl']) {
        unset($_SESSION['cache'][$key]);

        return $default;
    }

    return $item['v'];
}

/** cache_set 写缓存。 */
function cache_set(string $key, $value, int $ttl): void
{
    $_SESSION['cache'][$key] = array('at' => time(), 'ttl' => $ttl, 'v' => $value);
}

/** cache_forget 删缓存。 */
function cache_forget(string $key): void
{
    unset($_SESSION['cache'][$key]);
}

/**
 * cached 拿缓存，没有就调 $fn 生成并缓存，返回 array('v','at','fresh')。
 *
 * 命中时顺便把「什么时候拿的」带出来，页面可以显示，用户就知道看到的是不是刚拿的。
 */
function cached(string $key, int $ttl, callable $fn): array
{
    // 直接看会话里那条记录本身。不能拿 cache_get 的返回值去判断 —— 它返回的是
    // 内层的值，那种写法永远判不到，等于每次回源，缓存形同虚设。
    $item = isset($_SESSION['cache'][$key]) ? $_SESSION['cache'][$key] : null;
    if (is_array($item)
        && isset($item['at'], $item['ttl'])
        && time() - (int) $item['at'] <= (int) $item['ttl']) {
        return array('v' => $item['v'], 'at' => (int) $item['at'], 'fresh' => false);
    }

    $v = $fn();
    cache_set($key, $v, $ttl);

    return array('v' => $v, 'at' => time(), 'fresh' => true);
}

// ------------------------------------------------------------------- 会话

/** session_boot 起一个属于本门户的会话。 */
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
        'secure' => is_https(),
        'samesite' => 'Lax',
    ));
    session_start();
}

/** is_https 判断浏览器这一跳是不是 https。 */
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

/** session_forget 退出登录，顺带清掉这个人的缓存。 */
function session_forget(): void
{
    unset($_SESSION['aghub_token'], $_SESSION['aghub_user'], $_SESSION['cache']);
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

// ------------------------------------------------------------------- 访问者

/** client_ip 取访问者的地址。 */
function client_ip(): string
{
    $fwd = isset($_SERVER['HTTP_X_FORWARDED_FOR']) ? (string) $_SERVER['HTTP_X_FORWARDED_FOR'] : '';
    if ($fwd !== '') {
        $first = trim(explode(',', $fwd)[0]);
        if ($first !== '') {
            return $first;
        }
    }

    return isset($_SERVER['REMOTE_ADDR']) ? (string) $_SERVER['REMOTE_ADDR'] : '';
}

/**
 * visitor_ua 取访问者的 UA，只留可打印 ASCII。它要当 HTTP 头转发出去，非 ASCII
 * 字节会让 cURL 直接拒绝发请求。
 */
function visitor_ua(): string
{
    $ua = isset($_SERVER['HTTP_USER_AGENT']) ? (string) $_SERVER['HTTP_USER_AGENT'] : '';

    return (string) preg_replace('/[^\x20-\x7E]/', '', $ua);
}

/** ip_is_v6 判断是不是 IPv6。 */
function ip_is_v6(string $ip): bool
{
    return strpos($ip, ':') !== false;
}

/** device_name 从 UA 猜一个设备名，认不出来就返回空。 */
function device_name(): string
{
    $ua = isset($_SERVER['HTTP_USER_AGENT']) ? (string) $_SERVER['HTTP_USER_AGENT'] : '';
    if ($ua === '') {
        return '';
    }

    $map = array(
        'iPhone' => 'iPhone',
        'iPad' => 'iPad',
        'Android' => '安卓设备',
        'Windows' => 'Windows 电脑',
        'Macintosh' => 'Mac',
        'Linux' => 'Linux 设备',
    );

    foreach ($map as $needle => $name) {
        if (strpos($ua, $needle) !== false) {
            return $name;
        }
    }

    return '';
}

// ------------------------------------------------------------------- 显示

/** h 转义，所有输出都走它。 */
function h($v): string
{
    return htmlspecialchars((string) $v, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
}

/** num_h 给数字加千分位。 */
function num_h($n): string
{
    return number_format((float) $n);
}

/** big_h 把大数字压成 1.2亿 / 3456万 这种。 */
function big_h($n): string
{
    $n = (float) $n;
    if ($n < 10000) {
        return number_format($n);
    }

    if ($n < 100000000) {
        return rtrim(rtrim(number_format($n / 10000, 1), '0'), '.') . '万';
    }

    return rtrim(rtrim(number_format($n / 100000000, 2), '0'), '.') . '亿';
}

/** period_h 把配额周期变成人话。 */
function period_h(string $p): string
{
    $map = array('day' => '每天', 'week' => '每周', 'month' => '每月', 'year' => '每年', 'total' => '不限周期');

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
function remaining_h($sec): string
{
    $sec = (int) $sec;
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

    if ($d < 2592000) {
        return intdiv($d, 86400) . ' 天前';
    }

    return date('m-d', $ts);
}

/** pct 算百分比，夹在 0..100。 */
function pct($used, $limit): int
{
    $limit = (int) $limit;
    if ($limit <= 0) {
        return 0;
    }

    $p = (int) round((int) $used * 100 / $limit);

    return max(0, min(100, $p));
}

/**
 * verify_error 把「要邮箱验证码的账号操作」的错误翻成中文。
 *
 * 注册和绑定邮箱走的是同一套判定，所以共用一份。验证码那一条尤其要翻：
 * 接口只会说「验证码无效或已过期」，而用户很可能压根没收到过码。照抄英文
 * 原文，人会以为是自己填错了。
 *
 * $fallback 是认不出来的情况下的兜底文案，两个入口各说各的。
 */
function verify_error($r, string $fallback): string
{
    $err = is_array($r) && isset($r['error']) ? (string) $r['error'] : '';
    $status = is_array($r) ? (int) ($r['status'] ?? 0) : 0;
    $detail = is_array($r) && isset($r['detail']) ? (string) $r['detail'] : '';
    $hay = $detail !== '' ? $detail : $err;

    if (strpos($hay, 'verification code') !== false) {
        return '验证码不对或已经过期，请重新获取。';
    }

    if ($status === 503 || strpos($hay, 'mail server is not configured') !== false) {
        return '管理员还没有配置邮件服务器，现在注册不了。';
    }

    if (strpos($hay, 'already') !== false) {
        return '这个邮箱或用户名已经被占用了。';
    }

    if ($status === 429) {
        return '操作太频繁，等一会儿再试。';
    }

    return $err !== '' ? $err : $fallback;
}

/**
 * mail_code_message 把「发验证码」的结果翻成用户能看懂的话。
 *
 * 状态码是接口给的，话是门户自己说的 —— 把英文原文丢给用户没意义。
 */
function mail_code_message($r): string
{
    $status = is_array($r) ? (int) ($r['status'] ?? 0) : 0;

    switch ($status) {
        case 400:
            return '邮箱地址不对，检查一下。';
        case 429:
            return '发得太频繁了，等一分钟再试。';
        case 502:
            return '邮件没发出去，稍后再试。';
        case 503:
            return '管理员还没有配置邮件服务器。';
    }

    return '发不出去，请稍后再试。';
}

/**
 * rank_figure 取榜单一条记录在当前口径下的解析量。
 *
 * 两个口径的数字在接口里是两个字段，选哪个由页面决定。PHP 不自己算，
 * 免得「24 小时」和「累计」在两边各有一套定义。
 */
function rank_figure(array $e, string $order): int
{
    if ($order === 'total') {
        return (int) ($e['total_requests'] ?? 0);
    }

    return (int) ($e['requests_24h'] ?? 0);
}

/**
 * probe_owner 返回这台浏览器的匿名探测器标识，没有就生成一个。
 *
 * 未登录的访客也要能知道「手上这台设备有没有在用我们」，而探测器必须挂在
 * 一个标识下面：登录时用账号，未登录时用这个。它只活在这台浏览器的会话里，
 * 别人既猜不到也拿不到，所以查不出这台设备的结果。
 */
function probe_owner(): string
{
    if (empty($_SESSION['probe_owner'])) {
        $_SESSION['probe_owner'] = bin2hex(random_bytes(16));
    }

    return (string) $_SESSION['probe_owner'];
}

/**
 * probe_path 拼探测器接口的地址。
 *
 * 有会话就靠会话，AGHub 从会话里认出账号。没有会话就把这台浏览器的匿名标识
 * 作为 owner 带上；AGHub 那边会要求同时出示对接令牌，否则任何人都能往探测器
 * 表里塞东西。两者都没有就返回空串，调用方按「探测不了」处理。
 */
function probe_path(string $path, string $session, string $owner): string
{
    if ($session !== '') {
        return $path;
    }

    if ($owner === '') {
        return '';
    }

    $sep = strpos($path, '?') === false ? '?' : '&';

    return $path . $sep . 'owner=' . rawurlencode($owner);
}

/**
 * probe_label 向 AGHub 申请一个探测器，返回要解析的第一个标签。
 *
 * 网页读不到系统的 DNS 设置，所以只能反过来：让设备去解析一个随机名字，
 * 再看解析器那边有没有收到。名字里的随机串只有这一次页面加载知道，因此
 * 「收到」就等于「加载这个页面的设备在用我们」。
 *
 * 不走 fetch_all 的缓存 —— 探测器必须每次都是新的。
 */
function probe_label(string $session, string $owner = ''): string
{
    $path = probe_path('/portal/api/probe', $session, $owner);
    if ($path === '') {
        return '';
    }

    $r = aghub('POST', $path, array(), $session);
    if (!is_array($r) || empty($r['ok'])) {
        return '';
    }

    $label = isset($r['data']['label']) ? (string) $r['data']['label'] : '';

    // 只接受预期的形状，避免把任意内容拼进页面里的地址。
    if ($label === '' || strpos($label, 'aghub-probe-') !== 0) {
        return '';
    }

    return $label;
}

/**
 * probe_status 问 AGHub 探测器有没有命中。
 *
 * 只回一个布尔。不比对地址：同一 WiFi 下所有设备共用一个出口地址，拿它当判据
 * 就会把别的设备算成这一台。随机信物本身已经能定位到具体是哪台设备。
 */
function probe_status(string $label, string $session, string $owner = ''): array
{
    $miss = array('seen' => false);

    if ($label === '') {
        return $miss;
    }

    $path = probe_path('/portal/api/probe/status?token=' . rawurlencode($label), $session, $owner);
    if ($path === '') {
        return $miss;
    }

    $r = aghub('GET', $path, null, $session);
    if (!is_array($r) || empty($r['ok'])) {
        return $miss;
    }

    $d = isset($r['data']) && is_array($r['data']) ? $r['data'] : array();

    return array('seen' => !empty($d['seen']));
}

/**
 * site_host 取本站自己的主机名，用来给探测器拼一个子域名。
 *
 * 用自己域名的子域而不是第三方域名：不泄露给外人，也不会有 CSP 或混合内容
 * 的问题。IP 地址和单标签主机名拼不出子域，那种情况直接放弃探测。
 */
function site_host(): string
{
    $host = isset($_SERVER['HTTP_HOST']) ? (string) $_SERVER['HTTP_HOST'] : '';
    if ($host === '' || strpos($host, '.') === false) {
        return '';
    }

    if (filter_var($host, FILTER_VALIDATE_IP) !== false) {
        return '';
    }

    // 去掉端口。
    $host = explode(':', $host)[0];

    return $host;
}

/** dot_host 拼出接入主机名。 */
function dot_host(string $id, string $domain): string
{
    return ($domain === '' || $id === '') ? '' : $id . '.' . $domain;
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

/**
 * asset 给静态文件加版本号，改样式之后不会被浏览器缓存挡住。
 *
 * 用内容哈希，不用 filemtime：部署包里的每个文件都带着同一个固定时间戳
 * （打包时统一写成 1970，好让同样的源码每次打出来的字节完全一致），
 * 于是 filemtime 在任何版本里都是 0，版本号永远不变，
 * 浏览器就一直拿第一次缓存的 CSS —— 新 HTML 配旧样式，页面看着像坏了。
 * 内容一变哈希就变，缓存自然失效，也不怕别人用保留时间戳的方式复制文件。
 */
function asset(string $file): string
{
    $path = __DIR__ . '/' . $file;
    $v = is_file($path) ? substr(md5_file($path), 0, 8) : '1';

    return $file . '?v=' . $v;
}

/**
 * nav_items 返回底部导航的标签，顺序就是显示顺序，第一个是首页。
 */
function nav_items(): array
{
    return array(
        'report' => array('label' => '战报', 'icon' => 'chart'),
        'ranking' => array('label' => '排行榜', 'icon' => 'trophy'),
        'feedback' => array('label' => '反馈', 'icon' => 'feedback'),
        'me' => array('label' => '我的', 'icon' => 'user'),
    );
}

/** icon 输出一个线性图标。 */
function icon(string $name, string $class = ''): string
{
    $paths = array(
        'chart' => '<rect x="3.5" y="3.5" width="17" height="17" rx="3.5"/>'
            . '<path d="M8 16.5v-4M12 16.5v-8M16 16.5v-6"/>',
        'trophy' => '<path d="M7 4h10v4.5a5 5 0 0 1-10 0V4Z"/>'
            . '<path d="M7 6H4.5v1.5A2.5 2.5 0 0 0 7 10M17 6h2.5v1.5A2.5 2.5 0 0 1 17 10"/>'
            . '<path d="M12 13.5V17M8.5 20h7l-.7-3h-5.6l-.7 3Z"/>',
        'feedback' => '<path d="M4.5 5.5h15v11h-9L6 20v-3.5H4.5v-11Z"/>'
            . '<path d="M8.5 9.5h7M8.5 12.8h4.5"/>',
        'user' => '<circle cx="12" cy="8.5" r="3.6"/>'
            . '<path d="M4.8 20c.6-3.8 3.6-6 7.2-6s6.6 2.2 7.2 6"/>',
        'copy' => '<rect x="9" y="9" width="11" height="11" rx="2.5"/>'
            . '<path d="M15 6.5V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h.5"/>',
        'refresh' => '<path d="M20 12a8 8 0 1 1-2.4-5.7"/><path d="M20 4v4.5h-4.5"/>',
        'info' => '<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5M12 8v.1"/>',
        'shield' => '<path d="M12 3.5 19 6.5v5c0 4.4-3 8.2-7 9.5-4-1.3-7-5.1-7-9.5v-5l7-3Z"/>',
        'bolt' => '<path d="M13 3 5.5 13.5H11l-1 7.5 7.5-10.5H12l1-7.5Z"/>',
        'block' => '<circle cx="12" cy="12" r="8.5"/><path d="M6.5 6.5l11 11"/>',
        'check' => '<path d="M5 12.5 10 17.5 19 7"/>',
        'logout' => '<path d="M15 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h7a2 2 0 0 0 2-2v-2"/>'
            . '<path d="M10 12h11M18 8.5 21.5 12 18 15.5"/>',
        'key' => '<circle cx="8" cy="12" r="3.5"/><path d="M11.5 12H21M18 12v3M15 12v2"/>',
        'globe' => '<circle cx="12" cy="12" r="8.5"/>'
            . '<path d="M3.5 12h17M12 3.5c2.4 2.4 2.4 14.6 0 17M12 3.5c-2.4 2.4-2.4 14.6 0 17"/>',
        'clock' => '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>',
        'download' => '<path d="M12 3.5v11M8 11l4 4 4-4"/><path d="M4.5 19.5h15"/>',
        'send' => '<path d="M20.5 3.5 3.5 10.5l6.5 2.5 2.5 6.5 8-16Z"/><path d="M10 13l4-4"/>',
    );

    $body = isset($paths[$name]) ? $paths[$name] : '';

    return '<svg class="ic ' . h($class) . '" viewBox="0 0 24 24" fill="none" stroke="currentColor"'
        . ' stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">'
        . $body . '</svg>';
}

/**
 * avatar_html 画一个头像圆。
 *
 * 没设头像就退回名字首字，底色由标识推导 —— 同一个人每次刷新都是同一个颜色，
 * 不会跳来跳去。
 */
function avatar_html(string $avatar, string $name, string $id, string $cls = 'rk-ava'): string
{
    $seed = $id !== '' ? $id : $name;
    $hue = $seed !== '' ? (crc32($seed) % 360) : 210;

    $bg = 'hsl(' . $hue . ' 60% 92%)';
    $fg = 'hsl(' . $hue . ' 55% 32%)';

    $face = $avatar !== '' ? $avatar : mb_substr($name, 0, 1, 'UTF-8');
    if ($face === '') {
        $face = '?';
    }

    return '<span class="' . h($cls) . '" style="background:' . $bg . ';color:' . $fg . '">'
        . h($face) . '</span>';
}

/**
 * avatar_safe 把接口给的头像串收一下。
 *
 * 存下来的是用户选的字符串，渲染前必须过一遍：长度封顶，控制字符和尖括号
 * 一律不要。
 */
function avatar_safe($v): string
{
    $v = is_string($v) ? trim($v) : '';

    if ($v === '' || mb_strlen($v, 'UTF-8') > 4) {
        return '';
    }

    if (preg_match('/[<>\x00-\x1f]/u', $v)) {
        return '';
    }

    return $v;
}

/**
 * num_short 把数字压成图表里放得下的短写。
 *
 * 点上的数值必须一眼读完，所以过万就用「万」，别把 12,105 六个字符塞进去。
 */
function num_short(int $n): string
{
    if ($n >= 100000000) {
        return rtrim(rtrim(number_format($n / 100000000, 1), '0'), '.') . '亿';
    }

    if ($n >= 10000) {
        // 上百万就别留小数了，不然 7103.9万 这种七个字符塞不进点间距。
        if ($n >= 1000000) {
            return (string) (int) round($n / 10000) . '万';
        }

        return rtrim(rtrim(number_format($n / 10000, 1), '0'), '.') . '万';
    }

    return (string) $n;
}

/**
 * chart_color 按数值给点上色：越低越冷，越高越暖。
 *
 * 光看折线的高低，几个点挤在一起时分不出谁高谁低；给点本身染色，一眼就能
 * 看出哪几天是峰值。色相从 210（蓝）走到 20（橙）。
 */
function chart_color(int $v, int $peak, bool $for_text = false): string
{
    $t = $peak > 0 ? min(1, max(0, $v / $peak)) : 0;
    $hue = 210 - (int) round($t * 190);

    // 文字用的色要更深，浅色字压在浅底上读不清。
    return $for_text
        ? 'hsl(' . $hue . ' 68% 34%)'
        : 'hsl(' . $hue . ' 76% 47%)';
}

/**
 * linechart 画近 7 天的用量折线。
 *
 * 每个点上方标出具体数值、下方标出日期，两者都画在 SVG 里 —— 放 HTML 里就得
 * 靠百分比对齐，稍微一改宽度就错位。
 *
 * 视图不做 preserveAspectRatio="none" 拉伸：那样曲线能铺满整个宽度，但文字会
 * 被横向拉扁。改成等比缩放，宽度撑满、高度跟着走。
 */
function linechart(array $values, int $height = 172): string
{
    $n = count($values);
    if ($n === 0) {
        return '';
    }

    $vals = array_values($values);
    $labels = array_keys($values);

    $peak = max($vals);
    $peak = $peak > 0 ? $peak : 1;

    $w = 320;
    $padL = 22;
    $padR = 22;
    $padT = 28;   // 留给点上的数值
    $padB = 24;   // 留给点下的日期
    $plotW = $w - $padL - $padR;
    $plotH = $height - $padT - $padB;
    $step = $n > 1 ? ($plotW / ($n - 1)) : 0;

    $pts = array();
    foreach ($vals as $i => $v) {
        $x = $n > 1 ? $padL + $i * $step : $w / 2;
        $y = $padT + $plotH - ((int) $v * $plotH / $peak);
        $pts[] = array(round($x, 2), round($y, 2));
    }

    $line = array();
    foreach ($pts as $pt) {
        $line[] = $pt[0] . ',' . $pt[1];
    }

    $base = $padT + $plotH;
    $area = 'M' . $pts[0][0] . ',' . $base . ' L'
        . implode(' L', $line) . ' L' . $pts[$n - 1][0] . ',' . $base . ' Z';

    $out = '<svg class="lc" viewBox="0 0 ' . $w . ' ' . $height . '"'
        . ' role="img" aria-label="近 7 天用量趋势">';

    $out .= '<defs><linearGradient id="lcFill" x1="0" y1="0" x2="0" y2="1">'
        . '<stop offset="0%" stop-color="#2563eb" stop-opacity="0.20"/>'
        . '<stop offset="100%" stop-color="#2563eb" stop-opacity="0.02"/>'
        . '</linearGradient></defs>';

    // 三条参考线，让高度有个参照。
    for ($g = 1; $g <= 3; $g++) {
        $gy = round($padT + $plotH * $g / 4, 2);
        $out .= '<line x1="' . $padL . '" y1="' . $gy . '" x2="' . ($w - $padR) . '" y2="' . $gy . '"'
            . ' class="lc-grid"/>';
    }

    $out .= '<path d="' . $area . '" fill="url(#lcFill)"/>';
    $out .= '<polyline points="' . implode(' ', $line) . '" class="lc-line"/>';

    foreach ($pts as $i => $pt) {
        $v = (int) $vals[$i];
        $col = chart_color($v, $peak);
        $txt = chart_color($v, $peak, true);

        // 数值：点在下面时标上方，靠近顶部时标下方，免得顶出画布。
        $above = $pt[1] > ($padT + 11);
        $vy = $above ? $pt[1] - 9 : $pt[1] + 15;

        $out .= '<circle cx="' . $pt[0] . '" cy="' . $pt[1] . '" r="3.8"'
            . ' fill="' . $col . '" stroke="#fff" stroke-width="1.4" class="lc-dot">'
            . '<title>' . h((string) $labels[$i] . '：' . num_h($v) . ' 次') . '</title></circle>';

        $out .= '<text x="' . $pt[0] . '" y="' . round($vy, 2) . '" class="lc-val"'
            . ' fill="' . $txt . '">' . h(num_short($v)) . '</text>';

        // 日期压掉年份，只留 MM-DD。
        $day = (string) $labels[$i];
        if (strlen($day) >= 10) {
            $day = substr($day, 5);
        }
        $out .= '<text x="' . $pt[0] . '" y="' . ($height - 8) . '" class="lc-day">'
            . h($day) . '</text>';
    }

    $out .= '</svg>';

    return $out;
}

/**
 * mobileconfig 生成 iOS 的加密 DNS 描述文件。
 *
 * iOS 上装 DoT 不能像安卓那样填个主机名就完事，必须走 .mobileconfig。手工做
 * 一份容易写错 UUID 和键名，所以这里按标识现拼。
 *
 * UUID 由标识推导，不是随机的：同一台设备重复安装是更新而不是叠出好几份。
 */
function mobileconfig(string $id, string $domain, string $title = 'DNS 服务'): string
{
    $host = dot_host($id, $domain);
    if ($host === '') {
        return '';
    }

    // 由标识推导出稳定的 UUID（版本 5 风格的形状，够 iOS 用）。
    $seed = $host . '|aghub';
    $u = function (string $what) use ($seed): string {
        $h = md5($what . '|' . $seed);

        return substr($h, 0, 8) . '-' . substr($h, 8, 4) . '-' . substr($h, 12, 4)
            . '-' . substr($h, 16, 4) . '-' . substr($h, 20, 12);
    };

    $payload_uuid = $u('payload');
    $profile_uuid = $u('profile');

    $label = h($title . ' · ' . $id);

    $xml = '<?xml version="1.0" encoding="UTF-8"?>' . "\n"
        . '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"'
        . ' "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' . "\n"
        . '<plist version="1.0">' . "\n"
        . '<dict>' . "\n"
        . '  <key>PayloadContent</key>' . "\n"
        . '  <array>' . "\n"
        . '    <dict>' . "\n"
        . '      <key>DNSSettings</key>' . "\n"
        . '      <dict>' . "\n"
        . '        <key>DNSProtocol</key>' . "\n"
        . '        <string>TLS</string>' . "\n"
        . '        <key>ServerName</key>' . "\n"
        . '        <string>' . h($host) . '</string>' . "\n"
        . '        <key>ServerURL</key>' . "\n"
        . '        <string></string>' . "\n"
        . '      </dict>' . "\n"
        . '      <key>PayloadDescription</key>' . "\n"
        . '      <string>加密 DNS 解析，走 DoT。</string>' . "\n"
        . '      <key>PayloadDisplayName</key>' . "\n"
        . '      <string>' . $label . '</string>' . "\n"
        . '      <key>PayloadIdentifier</key>' . "\n"
        . '      <string>com.aghub.dns.' . h($id) . '</string>' . "\n"
        . '      <key>PayloadType</key>' . "\n"
        . '      <string>com.apple.dnsSettings.managed</string>' . "\n"
        . '      <key>PayloadUUID</key>' . "\n"
        . '      <string>' . $payload_uuid . '</string>' . "\n"
        . '      <key>PayloadVersion</key>' . "\n"
        . '      <integer>1</integer>' . "\n"
        . '    </dict>' . "\n"
        . '  </array>' . "\n"
        . '  <key>PayloadDescription</key>' . "\n"
        . '  <string>安装后本机 DNS 会走加密解析。</string>' . "\n"
        . '  <key>PayloadDisplayName</key>' . "\n"
        . '  <string>' . $label . '</string>' . "\n"
        . '  <key>PayloadIdentifier</key>' . "\n"
        . '  <string>com.aghub.profile.' . h($id) . '</string>' . "\n"
        . '  <key>PayloadOrganization</key>' . "\n"
        . '  <string>' . h($title) . '</string>' . "\n"
        . '  <key>PayloadRemovalDisallowed</key>' . "\n"
        . '  <false/>' . "\n"
        . '  <key>PayloadType</key>' . "\n"
        . '  <string>Configuration</string>' . "\n"
        . '  <key>PayloadUUID</key>' . "\n"
        . '  <string>' . $profile_uuid . '</string>' . "\n"
        . '  <key>PayloadVersion</key>' . "\n"
        . '  <integer>1</integer>' . "\n"
        . '</dict>' . "\n"
        . '</plist>' . "\n";

    return $xml;
}

/** sparkline 用 div 画一个迷你柱状图，不需要 canvas。 */
function sparkline(array $values, int $height = 46): string
{
    if ($values === array()) {
        return '';
    }

    $peak = 1;
    foreach ($values as $v) {
        if ((int) $v > $peak) {
            $peak = (int) $v;
        }
    }

    $out = '<div class="spark" style="height:' . (int) $height . 'px">';
    foreach ($values as $label => $v) {
        $p = (int) round((int) $v * 100 / $peak);
        if ($p < 4 && (int) $v > 0) {
            $p = 4;
        }

        $title = h((string) $label . '：' . num_h($v) . ' 次');

        $out .= '<div class="spark-col" title="' . $title . '">'
            . '<i style="height:' . (int) $p . '%"></i></div>';
    }
    $out .= '</div>';

    return $out;
}
