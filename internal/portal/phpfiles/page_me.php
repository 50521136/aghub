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

$dot = dot_host($primary_id, $domain);
$client = isset($user['client']) && is_array($user['client']) ? $user['client'] : array();
?>
<section class="me-head">
  <div class="me-avatar"><?= h(mb_substr((string) ($user['name'] ?? '?'), 0, 1, 'UTF-8')) ?></div>
  <div class="me-who">
    <div class="me-name"><?= h((string) ($user['name'] ?? '')) ?></div>
    <div class="me-meta">
      <span class="pill <?= $status === 'active' ? 'pill-ok' : 'pill-bad' ?>"><?= h(status_h($status)) ?></span>
      <span class="me-since"><?= h(since_h((int) ($user['last_seen'] ?? 0))) ?>活跃</span>
    </div>
  </div>
</section>

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
  </div>
</section>

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
