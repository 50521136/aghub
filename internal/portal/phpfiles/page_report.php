<?php
/**
 * 战报：首页。未登录也能看，登录后多出「我的用量」。
 *
 * 按你说的，首页不放配置那一块 —— 接入地址挪到「我的」里去了。
 */

declare(strict_types=1);

$queries = (int) ($public['queries'] ?? 0);
$rules = (int) ($public['rules'] ?? 0);
$accounts = (int) ($public['accounts'] ?? 0);
$active_accounts = (int) ($public['active'] ?? 0);
$lists = (int) ($public['lists'] ?? 0);
$custom = (int) ($public['custom_rules'] ?? 0);
$protected = !empty($public['protected']);

$device = device_name();
$v6 = ip_is_v6($visitor_ip);

// 登录之后才有个人用量。
$history = (isset($user['history']) && is_array($user['history'])) ? $user['history'] : array();
$points = array();
foreach ($history as $p) {
    if (!is_array($p)) {
        continue;
    }

    $day = isset($p['date']) ? (string) $p['date'] : '';
    $points[$day] = (int) ($p['requests'] ?? 0);
}
$points = array_slice($points, -7, 7, true);
?>
<section class="hero">
  <h1><?= $connected ? '广告正在被拦下' : ($logged ? '把设备接进来' : '加密 DNS 战报') ?></h1>
  <p class="hero-sub">
    <?php if ($connected): ?>
      解析正在加密传输，广告已经拦下
    <?php elseif ($logged): ?>
      复制专属地址填进设备，回到本页就能看到战报
    <?php else: ?>
      支持 DoT 与 DoH，登录后查看自己的标识与日志
    <?php endif; ?>
  </p>
</section>

<section class="card stat-card">
  <div class="stat-label">全站累计解析</div>
  <div class="stat-big" title="<?= h(num_h($queries)) ?>"><?= h(num_h($queries)) ?></div>
  <div class="stat-row">
    <div class="stat-mini">
      <span class="mk">过滤规则</span>
      <span class="mv"><?= h(num_h($rules)) ?></span>
    </div>
    <div class="stat-mini">
      <span class="mk">订阅账号</span>
      <span class="mv"><?= h(num_h($accounts)) ?></span>
    </div>
  </div>
  <div class="stat-foot">
    <div>
      <?php if ($protected): ?>
        <span class="tag tag-ok"><?= icon('check') ?>广告过滤运行中</span>
      <?php else: ?>
        <span class="tag tag-warn"><?= icon('info') ?>过滤未启用</span>
      <?php endif; ?>
    </div>
    <div class="stat-foot-txt">
      <?= h(num_h($lists)) ?> 个订阅源 · <?= h(num_h($custom)) ?> 条自定义规则
    </div>
  </div>
</section>

<?php if ($logged && $points !== array()): ?>
<section class="card">
  <div class="card-head">
    <h2>我的近 7 天</h2>
    <span class="card-note">柱高满格代表 <?= h(num_h(max($points))) ?> 次</span>
  </div>
  <?= sparkline($points) ?>
  <div class="spark-axis">
    <span><?= h((string) array_key_first($points)) ?></span>
    <span>今天</span>
  </div>
  <div class="spark-foot">
    <span>近 7 天共 <strong><?= h(num_h(array_sum($points))) ?></strong> 次</span>
    <a class="link" href="<?= h(page_url('log')) ?>">查看日志</a>
  </div>
</section>
<?php endif; ?>

<section class="card">
  <div class="card-head">
    <h2>当前访问</h2>
  </div>
  <div class="kv">
    <div class="kk"><?= icon('globe') ?>访问地址</div>
    <div class="vv"><code><?= h($visitor_ip !== '' ? $visitor_ip : '未知') ?></code></div>
  </div>
  <div class="kv">
    <div class="kk"><?= icon('bolt') ?>协议</div>
    <div class="vv"><?= $v6 ? 'IPv6' : 'IPv4' ?></div>
  </div>
  <?php if ($device !== ''): ?>
  <div class="kv">
    <div class="kk"><?= icon('phone') ?>设备</div>
    <div class="vv"><?= h($device) ?></div>
  </div>
  <?php endif; ?>
  <div class="kv">
    <div class="kk"><?= icon('key') ?>本站</div>
    <div class="vv">
      <?php if (is_https()): ?>
        <span class="ok-txt">HTTPS 已加密</span>
      <?php else: ?>
        <span class="warn-txt">HTTP 未加密</span>
      <?php endif; ?>
    </div>
  </div>
</section>

<section class="card">
  <div class="card-head">
    <h2>接入状态</h2>
    <span class="card-note">看查询记录自动判断</span>
  </div>
  <?php if ($connected): ?>
    <p class="hint">检测到你正在用本站解析，广告过滤已经生效。</p>
  <?php else: ?>
    <p class="hint">
      这个判断是看查询记录来的：只要设备在用 <?= h($host) ?> 解析，回到本页就会显示已接入。
      <?php if (!$logged): ?>
        登录之后可以在「我的」里拿到自己的专属地址。
      <?php else: ?>
        去「我的」复制专属地址。
      <?php endif; ?>
    </p>
  <?php endif; ?>
  <div class="tips">
    <div class="tip">
      <span class="tip-i"><?= icon('info') ?></span>
      <span>路由器上如果开了「DNS 代理 / 上网加速 / 智能选路」，它会覆盖下发的 DNS，设备看起来配好了却仍走运营商解析。先把那一项关掉。</span>
    </div>
  </div>
</section>

<?php if (!$logged): ?>
<section class="card cta">
  <div>
    <h2>登录查看自己的战报</h2>
    <p class="hint">登录后可以看到专属标识、专属接入地址、自己的查询日志。</p>
  </div>
  <a class="btn btn-primary" href="<?= h(page_url('me')) ?>">去登录</a>
</section>
<?php endif; ?>
