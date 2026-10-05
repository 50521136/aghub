<?php
/**
 * AGHub 门户 —— 入口。
 *
 * 整个页面由 PHP 渲染：浏览器只会请求本站，页面里没有任何对 AGHub 的
 * 浏览器端调用。所以既没有跨域，也没有混合内容 —— 这一跳是 PHP 在服务端
 * 完成的，http/https 都行，跟本站是不是 https 无关。
 */

declare(strict_types=1);

require_once __DIR__ . '/lib.php';

session_boot();

$page = isset($_GET['p']) ? (string) $_GET['p'] : '';
$flash_error = '';
$flash_ok = '';

// ------------------------------------------------------------------ 处理动作

if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    $action = isset($_POST['action']) ? (string) $_POST['action'] : '';

    if (!csrf_ok()) {
        $flash_error = '页面已过期，请重新提交。';
    } elseif ($action === 'login') {
        $login = trim(isset($_POST['login']) ? (string) $_POST['login'] : '');
        $password = isset($_POST['password']) ? (string) $_POST['password'] : '';

        if ($login === '' || $password === '') {
            $flash_error = '请填写用户名和密码。';
        } else {
            $r = aghub('POST', '/portal/api/login', array(
                'login' => $login,
                'password' => $password,
            ));

            if ($r['ok'] && !empty($r['data']['token'])) {
                session_regenerate_id(true);
                $_SESSION['aghub_token'] = (string) $r['data']['token'];
                unset($_SESSION['csrf']);
                redirect(page_url());
            }

            $flash_error = $r['error'] !== '' ? $r['error'] : '用户名或密码不对。';
        }
    } elseif ($action === 'logout') {
        aghub('POST', '/portal/api/logout', array(), session_token());
        session_forget();
        redirect(page_url());
    } elseif ($action === 'password') {
        if (session_token() === '') {
            $flash_error = '请先登录。';
        } else {
            $old = isset($_POST['old_password']) ? (string) $_POST['old_password'] : '';
            $new = isset($_POST['new_password']) ? (string) $_POST['new_password'] : '';
            $again = isset($_POST['new_password2']) ? (string) $_POST['new_password2'] : '';

            if ($new !== $again) {
                $flash_error = '两次输入的新密码不一样。';
            } elseif (strlen($new) < 8) {
                $flash_error = '新密码至少要 8 位。';
            } else {
                $r = aghub('POST', '/portal/api/password', array(
                    'old_password' => $old,
                    'new_password' => $new,
                ), session_token());

                if ($r['ok']) {
                    $flash_ok = '密码已修改。';
                } else {
                    $flash_error = $r['error'];
                }
            }
        }
    } elseif ($action === 'register') {
        $name = trim(isset($_POST['name']) ? (string) $_POST['name'] : '');
        $email = trim(isset($_POST['email']) ? (string) $_POST['email'] : '');
        $password = isset($_POST['password']) ? (string) $_POST['password'] : '';

        $r = aghub('POST', '/portal/api/register', array(
            'name' => $name,
            'email' => $email,
            'password' => $password,
        ));

        if ($r['ok']) {
            $flash_ok = '注册成功，现在可以登录了。';
            $page = '';
        } else {
            $flash_error = $r['error'];
        }
    }
}

// ------------------------------------------------------------------ 取数据

$logged = session_token() !== '';

// 公共统计：未登录也要看得到。
$pub = aghub('GET', '/portal/api/public');
$public = $pub['ok'] ? $pub['data'] : array();

// 站点配置（注册开关、公告）。
$cfg_r = aghub('GET', '/portal/api/config');
$site = $cfg_r['ok'] ? $cfg_r['data'] : array();

$user = array();
$log = array();

if ($logged) {
    $me = aghub('GET', '/portal/api/me', null, session_token());

    if ($me['ok']) {
        $user = isset($me['data']['user']) && is_array($me['data']['user']) ? $me['data']['user'] : array();
    } elseif ($me['status'] === 401) {
        // 会话在 AGHub 那边失效了。
        session_forget();
        $logged = false;
        $flash_error = '登录状态已失效，请重新登录。';
    } else {
        $flash_error = $me['error'];
    }
}

if ($logged && $page === 'log') {
    $limit = isset($_GET['limit']) ? (int) $_GET['limit'] : 50;
    if ($limit < 10 || $limit > 200) {
        $limit = 50;
    }

    $r = aghub('GET', '/portal/api/log?limit=' . $limit, null, session_token());
    if ($r['ok']) {
        $log = isset($r['data']['data']) && is_array($r['data']['data']) ? $r['data']['data'] : array();
    } else {
        $flash_error = $r['error'];
    }
}

$domain = isset($public['domain']) ? (string) $public['domain'] : '';
$ids = isset($user['ids']) && is_array($user['ids']) ? $user['ids'] : array();
$primary_id = $ids !== array() ? (string) $ids[0] : '';

// ------------------------------------------------------------------ 渲染

$title = (string) cfg('title', 'DNS 服务');
?>
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title><?= h($title) ?></title>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Crect width='32' height='32' rx='7' fill='%232563eb'/%3E%3Cpath d='M16 7l7 3v6c0 4.2-2.9 7.6-7 9-4.1-1.4-7-4.8-7-9v-6z' fill='none' stroke='white' stroke-width='2'/%3E%3C/svg%3E">
<link rel="stylesheet" href="style.css">
</head>
<body>
<header class="top">
  <div class="wrap">
    <a class="brand" href="<?= h(page_url()) ?>"><?= h($title) ?></a>
    <nav>
      <?php if ($logged): ?>
        <a href="<?= h(page_url()) ?>"<?= $page === '' ? ' class="on"' : '' ?>>概览</a>
        <a href="<?= h(page_url('log')) ?>"<?= $page === 'log' ? ' class="on"' : '' ?>>查询日志</a>
        <a href="<?= h(page_url('account')) ?>"<?= $page === 'account' ? ' class="on"' : '' ?>>账号</a>
        <form method="post" class="inline">
          <input type="hidden" name="action" value="logout">
          <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
          <button type="submit" class="link">退出</button>
        </form>
      <?php else: ?>
        <a href="#login">登录</a>
        <?php if (!empty($site['registration_open'])): ?>
          <a href="<?= h(page_url('register')) ?>">注册</a>
        <?php endif; ?>
      <?php endif; ?>
    </nav>
  </div>
</header>

<main class="wrap">
<?php if ($flash_error !== ''): ?>
  <div class="alert bad"><?= h($flash_error) ?></div>
<?php endif; ?>
<?php if ($flash_ok !== ''): ?>
  <div class="alert good"><?= h($flash_ok) ?></div>
<?php endif; ?>
<?php if (empty($public)): ?>
  <div class="alert bad">
    读不到 AGHub 的数据。<?= h($pub['error']) ?>
    <br>检查 config.php 里的 <code>aghub_url</code> 和 <code>token</code>。
  </div>
<?php endif; ?>
<?php if (isset($site['announcement']) && $site['announcement'] !== ''): ?>
  <div class="notice"><?= nl2br(h($site['announcement'])) ?></div>
<?php endif; ?>

<?php if ($page === 'log' && $logged): ?>
  <?php require __DIR__ . '/page_log.php'; ?>
<?php elseif ($page === 'account' && $logged): ?>
  <?php require __DIR__ . '/page_account.php'; ?>
<?php elseif ($page === 'register' && !$logged && !empty($site['registration_open'])): ?>
  <?php require __DIR__ . '/page_register.php'; ?>
<?php elseif ($logged): ?>
  <?php require __DIR__ . '/page_panel.php'; ?>
<?php else: ?>
  <?php require __DIR__ . '/page_public.php'; ?>
<?php endif; ?>
</main>

<footer class="wrap foot">
  <span>由 <strong>AGHub</strong> 提供 DNS 服务</span>
</footer>
</body>
</html>
