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

// 会话可能是空的（关过浏览器、闲置被回收），但浏览器里的长期令牌还在：先拿它
// 换一个新会话，用户就不会被弹回登录框。
remember_resume();

$nav = nav_items();

// 除了四个标签页，还有两个挂在它们下面的页面：日志在「我的」里，注册在登录流程里。
// 这两个不在导航表里，所以不能拿导航表当白名单。
$extra = array(
    'log' => 'me', 'register' => 'me', 'ios' => 'me',
    'probe' => 'me', 'avatar' => 'me', 'emailcode' => 'me',
);

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
        // 会话被回收之后再提交表单就会走到这里。现在有「保持登录」兜着，刷新
        // 一下会话就回来了，所以提示里说清楚要刷新。
        $flash_error = '页面已过期，请刷新后重新提交。';
    } elseif ($action === 'login') {
        $login = trim(isset($_POST['login']) ? (string) $_POST['login'] : '');
        $password = isset($_POST['password']) ? (string) $_POST['password'] : '';

        if ($login === '' || $password === '') {
            $flash_error = '请填写用户名和密码。';
        } else {
            $r = aghub('POST', '/portal/api/login', array(
                'login' => $login,
                'password' => $password,
                // 顺便要一个长期令牌：会话文件随时可能被 PHP 回收，令牌留在
                // 浏览器里才能让用户下次打开页面不用再登一次。
                'remember' => true,
            ));

            if ($r['ok'] && !empty($r['data']['token'])) {
                session_regenerate_id(true);
                $_SESSION['aghub_token'] = (string) $r['data']['token'];
                unset($_SESSION['csrf'], $_SESSION['cache']);
                remember_save(isset($r['data']['remember_token']) ? (string) $r['data']['remember_token'] : '');
                redirect(page_url('me'));
            }

            $flash_error = $r['error'] !== '' ? $r['error'] : '用户名或密码不对。';
        }
    } elseif ($action === 'logout') {
        // 先撤长期令牌再清会话：只清会话的话，下次打开页面又会被静默登回来，
        // 「退出登录」就成了假的。
        if (remember_token() !== '') {
            aghub('POST', '/portal/api/remember/forget', array('token' => remember_token()));
        }

        aghub('POST', '/portal/api/logout', array(), session_token());
        session_forget();
        remember_clear();
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
                    // 告诉服务端是哪台设备在改密码：其它设备会被撤掉，这台留着，
                    // 否则用户改完密码自己也掉线了。
                    'remember_token' => remember_token(),
                ), session_token());

                if ($r['ok']) {
                    $flash_ok = '密码已修改，其它设备已退出登录。';
                } else {
                    $flash_error = $r['error'];
                }
            }
        }
    } elseif ($action === 'remember') {
        if (session_token() === '') {
            $flash_error = '请先登录。';
        } else {
            $sub = isset($_POST['sub']) ? (string) $_POST['sub'] : '';

            if ($sub === 'all') {
                // 退出其它设备，但留下当前这台：它刚证明了自己是谁。
                $r = aghub('POST', '/portal/api/remember/revoke', array(
                    'all' => true,
                    'keep' => remember_hash(remember_token()),
                ), session_token());
            } else {
                $r = aghub('POST', '/portal/api/remember/revoke', array(
                    'id' => isset($_POST['id']) ? (string) $_POST['id'] : '',
                ), session_token());
            }

            if ($r['ok']) {
                // 列表是刚被改过的那份，别读缓存。
                cache_forget_scoped('devices');
                $flash_ok = $sub === 'all' ? '其它设备已退出登录。' : '该设备已退出登录。';
            } else {
                $flash_error = $r['error'];
            }
        }
    } elseif ($action === 'feedback') {
        if (session_token() === '') {
            $flash_error = '请先登录再反馈。';
        } else {
            $content = trim(isset($_POST['content']) ? (string) $_POST['content'] : '');
            $contact = trim(isset($_POST['contact']) ? (string) $_POST['contact'] : '');

            // 勾选框没勾就不会出现在 $_POST 里，所以「公开」按有没有这个键判断，
            // 而不是按它的值：表单默认是勾上的，也就是默认公开。
            $public = isset($_POST['public']);

            if ($content === '') {
                $flash_error = '写点内容吧。';
            } else {
                $r = aghub('POST', '/portal/api/feedback', array(
                    'content' => $content,
                    'contact' => $contact,
                    'public'  => $public,
                ), session_token());

                if ($r['ok']) {
                    // 刚提交的那条要立刻出现在下面的墙里，所以别读缓存。
                    cache_forget_scoped('wall');
                    $flash_ok = $public
                        ? '收到了，谢谢反馈。'
                        : '收到了，谢谢反馈。这条没有公开，只有你自己和管理员看得到。';
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

        $code = trim(isset($_POST['code']) ? (string) $_POST['code'] : '');

        $body = array(
            'name' => $name,
            'email' => $email,
            'password' => $password,
        );

        // 邮箱验证关着的时候表单不渲染验证码，这里也就不送。送空串和整个不送
        // 对接口是一回事，但少一个字段更干净。
        if ($code !== '') {
            $body['code'] = $code;
        }

        $r = aghub('POST', '/portal/api/register', $body);

        if ($r['ok']) {
            $flash_ok = '注册成功，用刚才的用户名登录即可。';
            $page = 'me';
        } else {
            $flash_error = verify_error($r, '注册失败，请稍后再试。');
        }
    } elseif ($action === 'email') {
        // 绑邮箱和注册用的是同一套验证码。空地址就是解绑 —— 接口那边不需要
        // 验证码，因为解绑不泄露任何东西。
        //
        // 判登录要用 session_token()：$logged 是后面「取数据」那一段才算出来的，
        // 动作块里读它永远是 null，于是登录着的人也会被告知「请先登录」。
        if (session_token() === '') {
            $flash_error = '请先登录。';
        } else {
            $addr = trim(isset($_POST['email']) ? (string) $_POST['email'] : '');
            $code = trim(isset($_POST['code']) ? (string) $_POST['code'] : '');

            $r = aghub('POST', '/portal/api/email', array(
                'email' => $addr,
                'code'  => $code,
            ), session_token());

            if (!empty($r['ok'])) {
                $flash_ok = $addr === '' ? '已解除邮箱绑定。' : '邮箱已更新。';
            } else {
                $flash_error = verify_error($r, '保存失败，请稍后再试。');
            }
        }
    } elseif ($action === 'checkin') {
        // 签到只做转发：送多少额度、连续几天、什么时候解锁日志，全部由 AGHub
        // 判定。这边再抄一份规则，两边迟早会不一致。
        if (session_token() === '') {
            $flash_error = '请先登录再签到。';
        } else {
            $r = aghub('POST', '/portal/api/checkin', array(), session_token());

            if (!empty($r['ok'])) {
                $d = isset($r['data']) && is_array($r['data']) ? $r['data'] : array();
                $days = isset($d['streak']) ? (int) $d['streak'] : 0;
                $bonus = isset($d['temp_bonus']) ? (int) $d['temp_bonus'] : 0;
                $perm = isset($d['permanent_bonus']) ? (int) $d['permanent_bonus'] : 0;

                $flash_ok = '签到成功，已连续 ' . $days . ' 天。';
                if ($bonus > 0) {
                    $flash_ok .= '今天多了 ' . num_h($bonus) . ' 次额度。';
                }

                if ($perm > 0) {
                    $flash_ok .= '达成 ' . $days . ' 天里程碑，永久 +' . num_h($perm) . ' 次。';
                }
            } else {
                $flash_error = $r['error'];
            }
        }
    }
}

// 刷新日志时跳过缓存。缓存键现在带会话前缀（s…:log:…），所以匹配中间的
// log: 而不是开头。
if (isset($_GET['fresh'])) {
    foreach (array_keys(isset($_SESSION['cache']) ? $_SESSION['cache'] : array()) as $k) {
        if (strpos((string) $k, 'log:') !== false) {
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

// 榜单有两种口径：今日（北京时间 0 点起算）和累计。默认今日 —— 榜单是用来
// 回答「现在谁在用」的，累计榜只会越来越慢地动。认不出的取值一律回到默认。
// 旧链接带的是 order=24h，那是这个标签页从前的名字，同样落到今日。
$rank_order = (isset($_GET['order']) && (string) $_GET['order'] === 'total') ? 'total' : 'today';

// 一页要问 AGHub 的三四个接口一次发出去，不要串行等。
// 缓存键带上日志的查询条件，换搜索词或条数就是另一次查询。
//
// 第三个元素 true 表示「这份响应跟访客无关」，放共享缓存给所有人用：榜单、
// 站点配置、头像预设这些，一个访客取过之后其他人就不用再回源了。带会话令牌
// 的那些不能共享，fetch_all 会自动按会话隔离。
$want = array(
    'public' => array(array('GET', '/portal/api/public'), 30, true),
    'config' => array(array('GET', '/portal/api/config'), 120, true),
);
if ($logged) {
    // 「我的」这份数据每个页面都要，但它随时会变（签到、兑换、踢设备）。
    // GET 时缓几秒省一次回源；POST 之后一律重新取，免得刚点完还显示旧状态。
    $me_ttl = $_SERVER['REQUEST_METHOD'] === 'POST' ? -1 : 5;
    $want['me'] = array(array('GET', '/portal/api/me', null, $session), $me_ttl);
}
if ($logged && $page === 'log') {
    $want['log:' . md5($log_q)] = array(array('GET', $log_q, null, $session), 45);
}
if ($logged && $page === 'me') {
    // 预设列表由 AGHub 单一来源提供，PHP 不自己抄一份。它基本不变，缓存可以长。
    $want['avatars'] = array(array('GET', '/portal/api/avatar'), 600, true);

    if (isset($_GET['view']) && $_GET['view'] === 'devices') {
        // 设备列表只在那一页取：它是一次真实的网络往返，不该让每个「我的」页
        // 都付这个成本。TTL 给 -1（不读缓存）—— 刚踢完设备看到的必须是新列表。
        $want['devices'] = array(array('GET', '/portal/api/remember', null, $session), -1);
    }
}
if ($page === 'ranking') {
    // 缓存键要带上口径，否则切到累计榜会读到今日榜的缓存。
    // 60 秒跟页面上写的「每 60 秒更新」对齐；这份数据所有人看到的一样，所以
    // 放共享缓存，一分钟里只有第一个访客真的回源。
    $want['ranking:' . $rank_order] = array(
        array('GET', '/portal/api/ranking?limit=30&order=' . $rank_order), 60, true,
    );
}
if ($page === 'feedback') {
    // 反馈墙：公开的留言，加上自己的留言（自己的那条无论公开与否都看得到）。
    // 未登录也取，因为公开那部分访客本来就该看得到。它带会话令牌，所以不能
    // 共享，只能按会话缓存。
    $want['wall'] = array(array('GET', '/portal/api/feedback?limit=50', null, $session), 45);
}

$got = fetch_all($want);

$pub_r = isset($got['public']) ? $got['public']['v'] : array();
$public = (is_array($pub_r) && !empty($pub_r['ok'])) ? $pub_r['data'] : array();

$cfg_r = isset($got['config']) ? $got['config']['v'] : array();
$site = (is_array($cfg_r) && !empty($cfg_r['ok'])) ? $cfg_r['data'] : array();

$user = array();
$checkin = array();
$log = array();
$log_at = 0;
$log_fresh = true;
$log_locked = false;

if ($logged && isset($got['me'])) {
    $me = $got['me']['v'];

    if (!empty($me['ok'])) {
        $user = isset($me['data']['user']) && is_array($me['data']['user']) ? $me['data']['user'] : array();
        $checkin = isset($me['data']['checkin']) && is_array($me['data']['checkin'])
            ? $me['data']['checkin'] : array();
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
    } elseif (is_array($r) && (int) ($r['status'] ?? 0) === 403) {
        // 日志要签到够了才开。这不是出错，所以不进错误提示条 —— 页面上会直接
        // 说清楚还差几天。
        $log_locked = true;
    } elseif (is_array($r)) {
        $flash_error = (string) $r['error'];
    }
}

$devices = array();
if ($logged && isset($got['devices'])) {
    $dev_r = $got['devices']['v'];
    if (is_array($dev_r) && !empty($dev_r['ok'])) {
        $devices = isset($dev_r['data']['devices']) && is_array($dev_r['data']['devices'])
            ? $dev_r['data']['devices'] : array();
    }
}

$ava_r = isset($got['avatars']) ? $got['avatars']['v'] : array();
$avatars = array();
if (is_array($ava_r) && !empty($ava_r['ok'])) {
    $presets = isset($ava_r['data']['presets']) && is_array($ava_r['data']['presets'])
        ? $ava_r['data']['presets'] : array();
    foreach ($presets as $a) {
        $a = avatar_safe($a);
        if ($a !== '') {
            $avatars[] = $a;
        }
    }
}

$rank_r = isset($got['ranking:' . $rank_order]) ? $got['ranking:' . $rank_order]['v'] : array();
$entries = array();
if (is_array($rank_r) && !empty($rank_r['ok'])) {
    $entries = isset($rank_r['data']['entries']) && is_array($rank_r['data']['entries'])
        ? $rank_r['data']['entries'] : array();
}

// 反馈墙分两段渲染：自己的一条单独放在上面（未公开的也只在这里出现），下面是
// 别人的公开留言。分不分由页面决定，这里只把数据摊平。
$wall_r = isset($got['wall']) ? $got['wall']['v'] : array();
$wall_items = array();
$wall_ok = true;
if (is_array($wall_r) && !empty($wall_r['ok'])) {
    $wall_items = isset($wall_r['data']['items']) && is_array($wall_r['data']['items'])
        ? $wall_r['data']['items'] : array();
} elseif (is_array($wall_r) && $wall_r !== array()) {
    $wall_ok = false;
}

$my_feedback = array();
$other_feedback = array();
foreach ($wall_items as $f) {
    if (!is_array($f)) {
        continue;
    }

    if (!empty($f['mine'])) {
        $my_feedback[] = $f;
    } else {
        $other_feedback[] = $f;
    }
}

// 登录之后申请一个探测器：页面把要解析的名字发给浏览器，随后回来问结果。
// 这一步不走并发批次，因为它绝不能命中缓存 —— 缓存下来的探测器会被复用，
// 探测就失去意义了。
$probe_label = '';
$probe_host = '';

// 探测器必须挂在 DNS 服务自己的域名下，**不能**挂在门户的域名下。
//
// 门户跑在用户自己的站点上，而探测靠的是「让设备去解析一个随机名字」。
// 设备如果不是走 AGHub，这个查询就会交给运营商或公共解析器 ——
// 名字里带着门户域名，等于把「这个人访问了哪个门户」告诉上游。
// 服务域名本来就是对外的 DNS 接入地址，不带任何关于访问者的信息。
//
// 服务域名没配就不探测。宁可显示「判断不了」，也不拿门户域名去换一个状态。
$probe_host = isset($public['domain']) ? trim((string) $public['domain']) : '';
if ($probe_host !== '' && strpos($probe_host, '.') === false) {
    $probe_host = '';
}

// 未登录的访客同样值得知道「手上这台设备有没有在用我们」—— 那正是决定他要
// 不要配一下的东西。所以未登录也发探测器，只是挂在浏览器的匿名标识下。
$anon_owner = $logged ? '' : probe_owner();

if ($probe_host !== '') {
    $probe_label = probe_label($session, $anon_owner);
}

$domain = isset($public['domain']) ? (string) $public['domain'] : '';
$ids = isset($user['ids']) && is_array($user['ids']) ? $user['ids'] : array();
$primary_id = $ids !== array() ? (string) $ids[0] : '';
$my_avatar = avatar_safe($user['avatar'] ?? '');
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

    $r = probe_status(isset($_GET['t']) ? (string) $_GET['t'] : '', $session, $anon_owner);

    echo json_encode($r, JSON_UNESCAPED_UNICODE);
    exit;
}

// 发邮箱验证码。浏览器不直接找 AGHub，所以这里替它转发一次 —— 门户的整个
// 设计就是浏览器只跟自己站点说话。
//
// 这个接口不需要登录（注册还没有账号），所以必须卡 CSRF：不卡的话，任何页面
// 都能拿我们的站点当发信机用。频率由 AGHub 按来访 IP 限。
if ($page === 'emailcode') {
    header('Content-Type: application/json; charset=utf-8');
    header('Cache-Control: no-store');

    $out = array('ok' => false, 'error' => '发不出去，请稍后再试。');

    if (!csrf_ok()) {
        $out['error'] = '页面已过期，刷新后重试。';
    } else {
        $addr = trim(isset($_POST['email']) ? (string) $_POST['email'] : '');
        $r = aghub('POST', '/portal/api/email/code', array('email' => $addr));

        if (is_array($r) && !empty($r['ok'])) {
            $out = array('ok' => true, 'error' => '');
        } else {
            $out['error'] = mail_code_message($r);
        }
    }

    echo json_encode($out, JSON_UNESCAPED_UNICODE);
    exit;
}

// 换头像。预设列表在 AGHub 那边，这里只做转发 —— 合法性也由它判定，
// 免得两边各有一份白名单。
if ($page === 'avatar') {
    header('Content-Type: application/json; charset=utf-8');
    header('Cache-Control: no-store');

    if (!$logged) {
        http_response_code(401);
        echo json_encode(array('ok' => false, 'error' => '请先登录'), JSON_UNESCAPED_UNICODE);
        exit;
    }

    if (!csrf_ok()) {
        http_response_code(400);
        echo json_encode(array('ok' => false, 'error' => '页面已过期，刷新后重试'), JSON_UNESCAPED_UNICODE);
        exit;
    }

    $want = avatar_safe(isset($_POST['avatar']) ? (string) $_POST['avatar'] : '');

    $r = aghub('POST', '/portal/api/avatar', array('avatar' => $want), $session);
    if (!is_array($r) || empty($r['ok'])) {
        http_response_code(502);
        echo json_encode(
            array('ok' => false, 'error' => is_array($r) ? (string) $r['error'] : '服务暂时不可用'),
            JSON_UNESCAPED_UNICODE
        );
        exit;
    }

    echo json_encode(array('ok' => true, 'avatar' => $want), JSON_UNESCAPED_UNICODE);
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

// 会话到这里就用完了：登录态读完了，缓存也不再往会话里写（缓存走上面那套带
// 会话前缀的存储）。现在放掉会话锁，同一个浏览器并发的第二个请求就不用排在
// 这个请求后面等 —— 「点一下要等一会儿」里有一部分就是排这个队。
//
// CSRF 令牌必须先取一次：页面渲染时要用它，而关了会话之后再写进 $_SESSION
// 的值不会保存，提交时校验就会失败。
csrf_token();
session_write_close();

$title = (string) cfg('title', 'DNS 服务');

// 标题里的页面名。导航表只有四个标签页，日志和注册挂在它们下面，所以不能直接
// 查 $nav[$page] —— 查不到就是一条 PHP 通知（页面照样出，日志里全是噪音）。
$extra_title = array('log' => '查询日志', 'register' => '注册', 'ios' => '接入');
$page_label = isset($nav[$page]['label'])
    ? (string) $nav[$page]['label']
    : (isset($extra_title[$page]) ? $extra_title[$page] : (string) $nav[$tab]['label']);
$host = $domain !== '' ? $domain : (isset($_SERVER['HTTP_HOST']) ? (string) $_SERVER['HTTP_HOST'] : '');
?>
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#eef4fd">
<title><?= h($title) ?> · <?= h($page_label) ?></title>
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
    <?php else: ?>
      <a class="topbar-user" href="<?= h(page_url('me')) ?>">
        <?= avatar_html($my_avatar, (string) ($user['name'] ?? ''), $primary_id, 'topbar-ava') ?>
        <span><?= h((string) ($user['name'] ?? '')) ?></span>
      </a>
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
      读不到 AGHub 的数据。<?= h(isset($pub_r['error']) ? (string) $pub_r['error'] : '') ?>
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
/* 换头像。
 *
 * 点一下就把选的图案 POST 给服务端，成功后就地更新头像圆和选中态 —— 不整页
 * 刷新，否则「我的」上那一堆卡片会闪一下再回来。
 */
(function () {
  var grid = document.querySelector('[data-ava-grid]');
  if (!grid) { return; }

  var msg = document.querySelector('[data-ava-msg]');
  var head = document.querySelector('[data-me-ava]');
  var busy = false;

  function say(text, bad) {
    if (!msg) { return; }
    msg.textContent = text;
    msg.hidden = !text;
    msg.className = bad ? 'hint hint-bad' : 'hint hint-ok';
  }

  grid.addEventListener('click', function (ev) {
    var btn = ev.target.closest ? ev.target.closest('.ava-pick') : null;
    if (!btn || busy) { return; }

    var val = btn.getAttribute('data-ava') || '';
    busy = true;
    say('', false);

    fetch(location.pathname + '?p=avatar', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'csrf=' + encodeURIComponent(grid.getAttribute('data-csrf') || '')
          + '&avatar=' + encodeURIComponent(val),
      credentials: 'same-origin'
    })
      .then(function (r) { return r.json(); })
      .then(function (d) {
        busy = false;

        if (!d || !d.ok) {
          say((d && d.error) || '换头像失败', true);
          return;
        }

        var picks = grid.querySelectorAll('.ava-pick');
        for (var i = 0; i < picks.length; i++) {
          picks[i].classList.toggle('on', picks[i].getAttribute('data-ava') === val);
        }

        if (head) { head.textContent = val; }
        say('已换成这个头像', false);
      })
      .catch(function () {
        busy = false;
        say('网络不通，稍后再试', true);
      });
  });
})();

/* 探测的重试阶梯。
 *
 * 浏览器解析随机名字是「发出探测」，这里回头问服务端有没有收到。分几次问是
 * 因为 DoT 往返可能慢一点，一次问不到就误判成未接入。命中即停。
 *
 * 判定只看这个随机名字有没有到达解析器：它只有本次页面加载知道，所以命中的
 * 一定是这台设备。地址不参与判断，同 WiFi 下的别的设备也就不会算进来。
 */
(function () {
  var pill = document.querySelector('[data-probe-pill]');
  if (!pill) { return; }

  var label = pill.getAttribute('data-probe-label') || '';
  var text = pill.querySelector('[data-probe-text]');
  if (!label || !text) { return; }

  var delays = [500, 1000, 2000];
  var step = 0;

  function settle(seen) {
    if (seen) {
      pill.className = 'pill pill-ok';
      text.textContent = '已接入';

      /* 战报页的标题跟着一起改，否则顶栏说已接入、正文还在教人怎么配置。 */
      var head = document.querySelector('[data-probe-head]');
      if (head) {
        head.textContent = head.getAttribute('data-probe-when-seen') || head.textContent;
      }

      return;
    }

    /* 重试都用完了还没命中，就是这台设备没接进来。
       不拿账号的活跃度兜底 —— 那会让同一 WiFi 下的别的设备把这一台点亮。 */
    pill.className = 'pill pill-idle';
    text.textContent = '未接入';
    /* 没命中就保持服务端给的「有设备在用 / 未接入」：浏览器用自己的
       安全 DNS 时探测本来就不会命中，那时降级比给错误答案好。 */
  }

  function ask() {
    fetch('?p=probe&t=' + encodeURIComponent(label), { cache: 'no-store' })
      .then(function (r) { return r.json(); })
      .then(function (d) {
        if (d && d.seen) { settle(true); return; }
        if (step < delays.length) { setTimeout(ask, delays[step++]); return; }

        settle(false);
      })
      .catch(function () {
        if (step < delays.length) { setTimeout(ask, delays[step++]); return; }

        settle(false);
      });
  }

  setTimeout(ask, delays[step++]);
})();

/* 注册页的「获取验证码」。点下去要等 AGHub 真的把信发出去，所以请求期间禁用
   按钮，成功后 60 秒才能再点 —— 接口那边另外按 IP 限流，两道都要有。 */
(function () {
  var btn = document.querySelector('[data-code-btn]');
  if (!btn) { return; }

  var hint = document.querySelector('[data-code-hint]');
  var form = btn.closest('form');
  var mail = form ? form.querySelector('input[name="email"]') : null;
  var field = form ? form.querySelector('input[name="code"]') : null;
  var left = 0;

  function say(text, bad) {
    if (!hint) { return; }
    hint.textContent = text;
    hint.className = bad ? 'hint hint-bad' : 'hint';
  }

  function tick() {
    if (left <= 0) {
      btn.disabled = false;
      btn.textContent = '重新获取';
      return;
    }

    btn.disabled = true;
    btn.textContent = left + ' 秒后可重发';
    left -= 1;
    setTimeout(tick, 1000);
  }

  btn.addEventListener('click', function () {
    var email = mail ? mail.value.trim() : '';
    if (!email) {
      say('先填邮箱。', true);
      if (mail) { mail.focus(); }
      return;
    }

    btn.disabled = true;
    say('正在发送…', false);

    var body = new URLSearchParams();
    body.set('csrf', btn.getAttribute('data-csrf') || '');
    body.set('email', email);

    fetch('?p=emailcode', { method: 'POST', body: body, cache: 'no-store' })
      .then(function (r) { return r.json(); })
      .then(function (d) {
        if (d && d.ok) {
          say('验证码已发出，去邮箱里找 6 位数字。', false);
          if (field) { field.focus(); }
          left = 60;
          tick();
          return;
        }

        say((d && d.error) || '发不出去，请稍后再试。', true);
        btn.disabled = false;
      })
      .catch(function () {
        say('网络不好，没发出去。', true);
        btn.disabled = false;
      });
  });
})();
</script>
</body>
</html>
