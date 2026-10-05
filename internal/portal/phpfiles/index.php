<?php
/**
 * AGHub 门户 —— 入口。
 *
 * 整个页面由 PHP 渲染：浏览器只会请求本站，页面里没有任何对 AGHub 的浏览器端
 * 调用。所以既没有跨域，也没有混合内容 —— 这一跳是 PHP 在服务端完成的，
 * http/https 都行，跟本站是不是 https 无关。
 */

declare(strict_types=1);

require_once __DIR__ . '/lib.php';

session_boot();

$nav = nav_items();

// 除了四个标签页，还有两个挂在它们下面的页面：日志在「我的」里，注册在登录流程里。
// 这两个不在导航表里，所以不能拿导航表当白名单。
$extra = array('log' => 'me', 'register' => 'me', 'ios' => 'me', 'probe' => 'me');

$page = isset($_GET['p']) ? (string) $_GET['p'] : 'report';
if (!isset($nav[$page]) && !isset($extra[$page])) {
    $page = 'report';
}

// 高亮哪个标签：日志和注册都算「我的」。
$tab = isset($extra[$page]) ? $extra[$page] : $page;

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
                unset($_SESSION['csrf'], $_SESSION['cache']);
                redirect(page_url('me'));
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
    } elseif ($action === 'feedback') {
        if (session_token() === '') {
            $flash_error = '请先登录再反馈。';
        } else {
            $content = trim(isset($_POST['content']) ? (string) $_POST['content'] : '');
            $contact = trim(isset($_POST['contact']) ? (string) $_POST['contact'] : '');

            if ($content === '') {
                $flash_error = '写点内容吧。';
            } else {
                $r = aghub('POST', '/portal/api/feedback', array(
                    'content' => $content,
                    'contact' => $contact,
                ), session_token());

                if ($r['ok']) {
                    $flash_ok = '收到了，谢谢反馈。';
                    $page = 'feedback';
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
            $flash_ok = '注册成功，用刚才的用户名登录即可。';
            $page = 'me';
        } else {
            $flash_error = $r['error'];
        }
    }
}

// 刷新日志时跳过缓存。
if (isset($_GET['fresh'])) {
    foreach (array_keys(isset($_SESSION['cache']) ? $_SESSION['cache'] : array()) as $k) {
        if (strpos((string) $k, 'log:') === 0) {
            cache_forget((string) $k);
        }
    }
}

// ------------------------------------------------------------------ 取数据

$logged = session_token() !== '';
$session = session_token();

// 日志的查询条件（只有日志页才用得上）。
$limit = isset($_GET['limit']) ? (int) $_GET['limit'] : 50;
if ($limit < 20 || $limit > 200) {
    $limit = 50;
}
$term = isset($_GET['term']) ? trim((string) $_GET['term']) : '';
$log_q = '/portal/api/log?limit=' . $limit;
if ($term !== '') {
    $log_q .= '&term=' . rawurlencode($term);
}

// 一页要问 AGHub 的三四个接口一次发出去，不要串行等。
// 缓存键带上日志的查询条件，换搜索词或条数就是另一次查询。
$want = array(
    'public' => array(array('GET', '/portal/api/public'), 20),
    'config' => array(array('GET', '/portal/api/config'), 60),
);
if ($logged) {
    $want['me'] = array(array('GET', '/portal/api/me', null, $session), 0);
}
if ($logged && $page === 'log') {
    $want['log:' . md5($log_q)] = array(array('GET', $log_q, null, $session), 30);
}
if ($page === 'ranking') {
    $want['ranking'] = array(array('GET', '/portal/api/ranking?limit=30'), 60);
}

$got = fetch_all($want);

$pub_r = $got['public']['v'];
$public = (is_array($pub_r) && !empty($pub_r['ok'])) ? $pub_r['data'] : array();

$cfg_r = $got['config']['v'];
$site = (is_array($cfg_r) && !empty($cfg_r['ok'])) ? $cfg_r['data'] : array();

$user = array();
$log = array();
$log_at = 0;
$log_fresh = true;

if ($logged && isset($got['me'])) {
    $me = $got['me']['v'];

    if (!empty($me['ok'])) {
        $user = isset($me['data']['user']) && is_array($me['data']['user']) ? $me['data']['user'] : array();
    } elseif ((int) $me['status'] === 401) {
        session_forget();
        $logged = false;
        $flash_error = '登录状态已失效，请重新登录。';
    } else {
        $flash_error = (string) $me['error'];
    }
}

if ($logged && $page === 'log' && isset($got['log:' . md5($log_q)])) {
    $c = $got['log:' . md5($log_q)];
    $r = $c['v'];
    $log_at = (int) $c['at'];
    $log_fresh = (bool) $c['fresh'];

    if (is_array($r) && !empty($r['ok'])) {
        $log = isset($r['data']['data']) && is_array($r['data']['data']) ? $r['data']['data'] : array();
    } elseif (is_array($r)) {
        $flash_error = (string) $r['error'];
    }
}

$rank_r = isset($got['ranking']) ? $got['ranking']['v'] : array();
$entries = array();
if (is_array($rank_r) && !empty($rank_r['ok'])) {
    $entries = isset($rank_r['data']['entries']) && is_array($rank_r['data']['entries'])
        ? $rank_r['data']['entries'] : array();
}

// 登录之后申请一个探测器：页面把要解析的名字发给浏览器，随后回来问结果。
// 这一步不走并发批次，因为它绝不能命中缓存 —— 缓存下来的探测器会被复用，
// 探测就失去意义了。
$probe_label = '';
$probe_host = site_host();
if ($logged && $probe_host !== '') {
    $probe_label = probe_label($session);
}

$domain = isset($public['domain']) ? (string) $public['domain'] : '';
$ids = isset($user['ids']) && is_array($user['ids']) ? $user['ids'] : array();
$primary_id = $ids !== array() ? (string) $ids[0] : '';
$title_hint = (string) cfg('title', 'DNS 服务');

// 当前访问者是否已经通过加密 DNS 接进来了：拿他的地址和账号里记的比。
$client = isset($user['client']) && is_array($user['client']) ? $user['client'] : array();
$visitor_ip = client_ip();
$connected = false;
if ($logged && !empty($client['connected'])) {
    $connected = true;
}

// ------------------------------------------------------------------ 探测结果

// 浏览器回头问「刚才那个名字有没有到达解析器」。这是纯 JSON 端点，不能走到
// 页面渲染里去。
if ($page === 'probe') {
    header('Content-Type: application/json; charset=utf-8');
    header('Cache-Control: no-store');

    $r = probe_status(isset($_GET['t']) ? (string) $_GET['t'] : '', $session);

    echo json_encode($r, JSON_UNESCAPED_UNICODE);
    exit;
}

// ------------------------------------------------------------------ 描述文件

// iOS 的加密 DNS 要装一个描述文件。这里现拼一个给它下载，不用管理员手工做。
if ($page === 'ios') {
    if (!$logged || $primary_id === '') {
        redirect(page_url('me'));
    }

    $mc = mobileconfig($primary_id, $domain !== '' ? $domain : $host, $title_hint);

    header('Content-Type: application/x-apple-aspen-config; charset=utf-8');
    header('Content-Disposition: attachment; filename="dns.mobileconfig"');
    header('Content-Length: ' . strlen($mc));
    header('Cache-Control: no-store');

    echo $mc;
    exit;
}

// ------------------------------------------------------------------ 渲染

$title = (string) cfg('title', 'DNS 服务');
$host = $domain !== '' ? $domain : (isset($_SERVER['HTTP_HOST']) ? (string) $_SERVER['HTTP_HOST'] : '');
?>
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#eef4fd">
<title><?= h($title) ?> · <?= h($nav[$page]['label']) ?></title>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Crect width='32' height='32' rx='7' fill='%232563eb'/%3E%3Cpath d='M16 7l7 3v6c0 4.2-2.9 7.6-7 9-4.1-1.4-7-4.8-7-9v-6z' fill='none' stroke='white' stroke-width='2'/%3E%3C/svg%3E">
<link rel="stylesheet" href="<?= h(asset('style.css')) ?>">
</head>
<body>
<div class="shell">

  <header class="topbar">
    <div class="brand">
      <span class="brand-dot"></span>
      <span class="brand-name"><?= h($host !== '' ? $host : $title) ?></span>
    </div>
    <?php if (!$logged): ?>
      <a class="topbar-login" href="<?= h(page_url('me')) ?>">登录</a>
    <?php elseif ($probe_label !== ''): ?>
      <?php /* 先按账号级信号渲染，探测命中后再升级成「已接入」。
              这样即使浏览器不执行脚本，看到的也是诚实的说法而不是「检测中」。 */ ?>
      <span
        class="pill <?= $connected ? 'pill-soft' : 'pill-idle' ?>"
        data-probe-pill
        data-probe-label="<?= h($probe_label) ?>"
      ><?= icon($connected ? 'info' : 'info') ?><span data-probe-text><?= $connected ? '有设备在用' : '未接入' ?></span></span>
      <img
        class="probe-img"
        src="https://<?= h($probe_label . '.' . $probe_host) ?>/p.png"
        width="1" height="1" alt=""
      >
    <?php else: ?>
      <span class="pill <?= $connected ? 'pill-soft' : 'pill-idle' ?>"><?= icon('info') ?><?= $connected ? '有设备在用' : '未接入' ?></span>
    <?php endif; ?>
  </header>

  <main class="main">
  <?php if ($flash_error !== ''): ?>
    <div class="alert bad"><?= h($flash_error) ?></div>
  <?php endif; ?>
  <?php if ($flash_ok !== ''): ?>
    <div class="alert good"><?= h($flash_ok) ?></div>
  <?php endif; ?>
  <?php if (empty($public)): ?>
    <div class="alert bad">
      读不到 AGHub 的数据。<?= h(is_array($pub_r) ? (string) $pub_r['error'] : '') ?>
      <br>检查 config.php 里的 <code>aghub_url</code> 和 <code>token</code>。
    </div>
  <?php endif; ?>

  <?php
  switch ($page) {
      case 'ranking':
          require __DIR__ . '/page_ranking.php';
          break;
      case 'feedback':
          require __DIR__ . '/page_feedback.php';
          break;
      case 'me':
          require __DIR__ . '/page_me.php';
          break;
      case 'log':
          if (!$logged) {
              require __DIR__ . '/page_login.php';
          } else {
              require __DIR__ . '/page_log.php';
          }
          break;
      case 'register':
          if (!empty($site['registration_open'])) {
              require __DIR__ . '/page_register.php';
          } else {
              require __DIR__ . '/page_report.php';
          }
          break;
      default:
          require __DIR__ . '/page_report.php';
          break;
  }
  ?>
  </main>

  <nav class="tabbar">
    <?php foreach ($nav as $key => $item): ?>
      <a class="tab<?= $key === $tab ? ' on' : '' ?>" href="<?= h(page_url($key)) ?>">
        <span class="tab-bar"></span>
        <?= icon($item['icon']) ?>
        <span class="tab-label"><?= h($item['label']) ?></span>
      </a>
    <?php endforeach; ?>
  </nav>

</div>
<script>
/* 只有「复制」用到 JS —— 页面本身是 PHP 渲染好的，没有浏览器端接口调用。 */
function copyThis(btn) {
  var row = btn.closest('.copy-row');
  var text = row ? row.getAttribute('data-copy') : '';
  if (!text) return;
  var done = function () {
    var old = btn.textContent;
    btn.textContent = '已复制';
    setTimeout(function () { btn.textContent = old; }, 1500);
  };
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(done, function () { fallback(text, done); });
  } else {
    fallback(text, done);
  }
}
function fallback(text, done) {
  var ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.position = 'fixed';
  ta.style.left = '-9999px';
  document.body.appendChild(ta);
  ta.select();
  try { document.execCommand('copy'); done(); } catch (e) { alert('复制失败，请手动选中：' + text); }
  document.body.removeChild(ta);
}
</script>
<script>
/* 探测的重试阶梯。
 *
 * 浏览器解析随机名字是「发出探测」，这里回头问服务端有没有收到。分几次问是
 * 因为 DoT 往返可能慢一点，一次问不到就误判成未接入。命中即停。
 *
 * 服务端已经渲染了账号级的说法，所以这里的职责只有两个：命中时升级成
 * 「已接入」，以及确认未命中且账号也不活跃时落定为「未接入」。
 */
(function () {
  var pill = document.querySelector('[data-probe-pill]');
  if (!pill) { return; }

  var label = pill.getAttribute('data-probe-label') || '';
  var text = pill.querySelector('[data-probe-text]');
  if (!label || !text) { return; }

  var delays = [900, 1500, 2500, 4000];
  var step = 0;

  function settle(seen) {
    if (!seen) { return; }

    pill.className = 'pill pill-ok';
    text.textContent = '已接入';

    /* 战报页的标题跟着一起改，否则顶栏说已接入、正文还在教人怎么配置。 */
    var head = document.querySelector('[data-probe-head]');
    if (head) {
      head.textContent = head.getAttribute('data-probe-when-seen') || head.textContent;
    }
    /* 没命中就保持服务端给的「有设备在用 / 未接入」：浏览器用自己的
       安全 DNS 时探测本来就不会命中，那时降级比给错误答案好。 */
  }

  function ask() {
    fetch('?p=probe&t=' + encodeURIComponent(label), { cache: 'no-store' })
      .then(function (r) { return r.json(); })
      .then(function (d) {
        if (d && d.seen) { settle(true); return; }
        if (step < delays.length) { setTimeout(ask, delays[step++]); }
      })
      .catch(function () {
        if (step < delays.length) { setTimeout(ask, delays[step++]); }
      });
  }

  setTimeout(ask, delays[step++]);
})();
</script>
</body>
</html>
