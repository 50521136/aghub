<?php
/**
 * 我的：没登录就是登录页，登录之后是账号、专属标识和接入地址。
 *
 * 接入地址放在这里而不是首页 —— 首页留给你要的战报。
 */

declare(strict_types=1);

if (!$logged) {
    require __DIR__ . '/page_login.php';

    return;
}

$limit = (int) ($user['request_limit'] ?? 0);
$used = (int) ($user['requests'] ?? 0);
$remaining = (int) ($user['remaining_requests'] ?? 0);
$total = (int) ($user['total_requests'] ?? 0);
$status = (string) ($user['status'] ?? '');
$unlimited = $limit <= 0;
$p = $unlimited ? 0 : pct($used, $limit);
$bar = $p >= 90 ? ' danger' : ($p >= 70 ? ' warn' : '');
$period = (string) ($user['period'] ?? 'day');

// 邮箱：当前地址和验证状态显示在「账号」卡里，换绑在 ?view=email 里。
$me_mail = isset($user['email']) ? trim((string) $user['email']) : '';
$me_mail_ok = !empty($user['email_verified']);

$dot = dot_host($primary_id, $domain);
$client = isset($user['client']) && is_array($user['client']) ? $user['client'] : array();
?>
<section class="me-head">
  <div class="me-avatar" data-me-ava><?= h($my_avatar !== '' ? $my_avatar : mb_substr((string) ($user['name'] ?? '?'), 0, 1, 'UTF-8')) ?></div>
  <div class="me-who">
    <div class="me-name"><?= h((string) ($user['name'] ?? '')) ?></div>
    <div class="me-meta">
      <span class="pill <?= $status === 'active' ? 'pill-ok' : 'pill-bad' ?>"><?= h(status_h($status)) ?></span>
      <span class="me-since"><?= h(since_h((int) ($user['last_seen'] ?? 0))) ?>活跃</span>
    </div>
  </div>
</section>

<?php if ($avatars !== array()): ?>
<section class="card">
  <div class="card-head">
    <h2>头像</h2>
    <span class="card-note">点一下就换</span>
  </div>
  <div class="ava-grid" data-ava-grid data-csrf="<?= h(csrf_token()) ?>">
    <?php foreach ($avatars as $a): ?>
      <button type="button" class="ava-pick<?= $a === $my_avatar ? ' on' : '' ?>"
              data-ava="<?= h($a) ?>"><?= h($a) ?></button>
    <?php endforeach; ?>
  </div>
  <p class="hint" data-ava-msg hidden></p>
</section>
<?php endif; ?>

<section class="card">
  <div class="card-head">
    <h2>配额</h2>
    <span class="card-note"><?= h(period_h($period)) ?>重置</span>
  </div>
  <?php if ($unlimited): ?>
    <div class="quota-num"><strong>不限量</strong></div>
    <div class="quota-sub">累计已解析 <?= h(num_h($total)) ?> 次</div>
  <?php else: ?>
    <div class="quota-num">
      <strong><?= h(num_h($used)) ?></strong>
      <span>/ <?= h(num_h($limit)) ?></span>
    </div>
    <div class="bar"><i class="<?= h($bar) ?>" style="width:<?= (int) $p ?>%"></i></div>
    <div class="quota-sub">
      还剩 <?= h(num_h($remaining)) ?> 次 ·
      下次重置 <?= h(when_h((int) ($user['next_reset'] ?? 0))) ?>
    </div>
  <?php endif; ?>
  <div class="quota-grid">
    <div><span>累计解析</span><b><?= h(num_h($total)) ?></b></div>
    <div><span>有效期</span><b><?= h(remaining_h($user['remaining_seconds'] ?? -1)) ?></b></div>
  </div>
</section>

<section class="card">
  <div class="card-head">
    <h2>我的专属标识</h2>
    <span class="card-note">别人用不了</span>
  </div>
  <div class="id-big">
    <?php foreach ($ids as $i => $one): ?>
      <code class="id-chip"><?= h((string) $one) ?></code>
    <?php endforeach; ?>
    <?php if ($ids === array()): ?>
      <span class="hint">管理员还没给你分配标识。</span>
    <?php endif; ?>
  </div>

  <?php if ($dot !== ''): ?>
    <div class="copy-row" data-copy="<?= h($dot) ?>">
      <span class="copy-k">DoT 主机名</span>
      <span class="copy-v"><?= h($dot) ?></span>
      <button type="button" class="copy-b" onclick="copyThis(this)">复制</button>
    </div>
    <p class="hint">
      安卓在「私人 DNS」里填上面那个主机名就行。
      iPhone 装下面的描述文件，系统会自动把加密 DNS 配好。
    </p>
    <a class="btn btn-primary btn-block" href="<?= h(page_url('ios')) ?>">
      <?= icon('download') ?>生成 iPhone 描述文件
    </a>
    <p class="hint">描述文件只包含这一个解析地址，装完可以在「设置 → 通用 → VPN与设备管理」里随时删掉。</p>
  <?php else: ?>
    <p class="hint">AGHub 还没配置域名，所以暂时没有接入地址。</p>
  <?php endif; ?>
</section>

<section class="card">
  <div class="card-head">
    <h2>连接情况</h2>
    <span class="pill <?= !empty($client['connected']) ? 'pill-ok' : 'pill-idle' ?>">
      <?= !empty($client['connected']) ? '在线' : '未检测到' ?>
    </span>
  </div>
  <div class="kv">
    <div class="kk"><?= icon('clock') ?>最近活动</div>
    <div class="vv"><?= h(since_h((int) ($user['last_seen'] ?? 0))) ?></div>
  </div>
  <div class="kv">
    <div class="kk"><?= icon('globe') ?>来源地址</div>
    <div class="vv"><?= !empty($client['ip']) ? h((string) $client['ip']) : '—' ?></div>
  </div>
  <?php if (!empty($client['device'])): ?>
  <div class="kv">
    <div class="kk"><?= icon('bolt') ?>设备</div>
    <div class="vv"><?= h((string) $client['device']) ?></div>
  </div>
  <?php endif; ?>
</section>

<section class="card">
  <div class="card-head"><h2>快捷入口</h2></div>
  <div class="quick">
    <a class="quick-item" href="<?= h(page_url('log')) ?>"><?= icon('chart') ?><span>查询日志</span></a>
    <a class="quick-item" href="<?= h(page_url('ranking')) ?>"><?= icon('trophy') ?><span>排行榜</span></a>
    <a class="quick-item" href="<?= h(page_url('feedback')) ?>"><?= icon('feedback') ?><span>反馈</span></a>
    <a class="quick-item" href="<?= h(page_url('me', array('view' => 'password'))) ?>"><?= icon('key') ?><span>改密码</span></a>
    <a class="quick-item" href="<?= h(page_url('me', array('view' => 'email'))) ?>"><?= icon('send') ?><span>邮箱</span></a>
    <a class="quick-item" href="<?= h(page_url('me', array('view' => 'devices'))) ?>"><?= icon('device') ?><span>登录设备</span></a>
  </div>
</section>

<?php if (isset($_GET['view']) && $_GET['view'] === 'email'): ?>
<section class="card">
  <div class="card-head">
    <h2><?= $me_mail !== '' ? '更换邮箱' : '绑定邮箱' ?></h2>
    <span class="card-note"><?= $me_mail !== '' ? h($me_mail) : '还没绑' ?></span>
  </div>
  <p class="hint">
    要换的地址得真能收信 —— 验证码发过去，填回来才算数。
  </p>
  <form method="post">
    <input type="hidden" name="action" value="email">
    <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
    <label><span>新邮箱</span>
      <input type="email" name="email" autocomplete="email" maxlength="254" required></label>
    <label><span>验证码</span>
      <span class="field-row">
        <input type="text" name="code" inputmode="numeric" autocomplete="one-time-code"
               maxlength="6" pattern="[0-9]{6}" placeholder="6 位数字" required>
        <button type="button" class="btn" data-code-btn
                data-csrf="<?= h(csrf_token()) ?>">获取验证码</button>
      </span>
    </label>
    <p class="hint" data-code-hint>点「获取验证码」，邮件里会有 6 位数字，15 分钟内有效。</p>
    <button type="submit" class="btn btn-primary btn-block">保存</button>
  </form>
  <?php if ($me_mail !== ''): ?>
    <?php /* 解绑不需要验证码：解绑不泄露任何东西，也就不必证明地址是你的。 */ ?>
    <form method="post">
      <input type="hidden" name="action" value="email">
      <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
      <input type="hidden" name="email" value="">
      <button type="submit" class="btn">解除绑定</button>
    </form>
  <?php endif; ?>
</section>
<?php endif; ?>

<?php if (isset($_GET['view']) && $_GET['view'] === 'devices'): ?>
<?php
// 当前这台设备用令牌的哈希认出来 —— 列表里的 id 就是它，服务端从不下发令牌本身。
$dev_me = remember_hash(remember_token());
?>
<section class="card">
  <div class="card-head">
    <h2>登录设备</h2>
    <span class="card-note"><?= $devices === array() ? '一台都没有' : count($devices) . ' 台' ?></span>
  </div>
  <p class="hint">
    这些设备打开网站不用再输密码。令牌每用一次就换一个，列表里存的是它的指纹，
    不是令牌本身 —— 谁捡到这份列表也用不了。不认识的设备在这里退出就行。
  </p>
  <?php if ($devices !== array()): ?>
    <div class="log-list">
      <?php foreach ($devices as $d): ?>
        <?php
        $did = isset($d['id']) ? (string) $d['id'] : '';
        $dname = device_label(isset($d['user_agent']) ? (string) $d['user_agent'] : '');
        $dip = isset($d['ip']) && $d['ip'] !== '' ? (string) $d['ip'] : '地址未知';
        $dlast = isset($d['last_used']) ? (int) $d['last_used'] : 0;
        $dcur = ($dev_me !== '' && $did === $dev_me);
        ?>
        <div class="log-item">
          <div class="log-top">
            <span class="log-host"><?= h($dname !== '' ? $dname : '认不出的设备') ?></span>
            <?php if ($dcur): ?><span class="tag tag-ok">这台</span><?php endif; ?>
            <span class="log-meta-txt"><?= h($dip) ?> · <?= h(since_h($dlast)) ?></span>
            <?php if (!$dcur): ?>
              <span class="row-actions">
                <form method="post">
                  <input type="hidden" name="action" value="remember">
                  <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
                  <input type="hidden" name="id" value="<?= h($did) ?>">
                  <button type="submit" class="btn btn-wide">退出</button>
                </form>
              </span>
            <?php endif; ?>
          </div>
        </div>
      <?php endforeach; ?>
    </div>
    <form method="post">
      <input type="hidden" name="action" value="remember">
      <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
      <input type="hidden" name="sub" value="all">
      <button type="submit" class="btn btn-block">退出其它设备</button>
    </form>
  <?php endif; ?>
</section>
<?php endif; ?>

<?php if (isset($_GET['view']) && $_GET['view'] === 'password'): ?>
<section class="card">
  <div class="card-head"><h2>修改密码</h2></div>
  <form method="post">
    <input type="hidden" name="action" value="password">
    <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
    <label><span>当前密码</span>
      <input type="password" name="old_password" autocomplete="current-password" required></label>
    <label><span>新密码（至少 8 位）</span>
      <input type="password" name="new_password" autocomplete="new-password" required minlength="8"></label>
    <label><span>再输一次</span>
      <input type="password" name="new_password2" autocomplete="new-password" required minlength="8"></label>
    <button type="submit" class="btn btn-primary btn-block">保存</button>
  </form>
</section>
<?php endif; ?>

<section class="card">
  <div class="card-head"><h2>账号</h2></div>
  <div class="kv"><div class="kk">用户名</div><div class="vv"><?= h((string) ($user['name'] ?? '')) ?></div></div>
  <div class="kv">
    <div class="kk"><?= icon('send') ?>邮箱</div>
    <div class="vv">
      <?php if ($me_mail === ''): ?>
        未绑定
      <?php else: ?>
        <?= h($me_mail) ?>
        <?php if ($me_mail_ok): ?>
          <span class="tag tag-ok">已验证</span>
        <?php else: ?>
          <span class="tag">未验证</span>
        <?php endif; ?>
      <?php endif; ?>
    </div>
  </div>
  <div class="kv"><div class="kk">建立时间</div><div class="vv"><?= h(when_h((int) ($user['created_at'] ?? 0))) ?></div></div>
  <div class="kv"><div class="kk">到期时间</div>
    <div class="vv"><?= (int) ($user['expires_at'] ?? 0) > 0 ? h(when_h((int) $user['expires_at'])) : '长期有效' ?></div>
  </div>
  <form method="post" class="logout-form">
    <input type="hidden" name="action" value="logout">
    <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
    <button type="submit" class="btn btn-block"><?= icon('logout') ?>退出登录</button>
  </form>
</section>
