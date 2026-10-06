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
<?php
// 全站数据。统计模块没开时近 24 小时的三个键会缺席 —— 那就退回累计口径，
// 并把标题改成「累计」，而不是把整块藏起来。
//
// 早先的做法是拿不到 24 小时数据就整块不显示，理由是「一排 0 看起来像什么都
// 没拦」。但整块消失更糟：用户看到的是页面缺了一块，只会当成坏了。缺数据就
// 说清缺的是什么，别把信息藏掉。
$has24 = isset($public['queries_24h']);

$q = $has24 ? (int) $public['queries_24h'] : (int) ($public['queries'] ?? 0);
$b = $has24 ? (int) $public['blocked_24h'] : (int) ($public['blocked'] ?? 0);
$p = $has24 ? (int) $public['passed_24h'] : (int) ($public['passed'] ?? 0);
$rate = $q > 0 ? (int) round($b * 100 / $q) : 0;
$scope = $has24 ? '近 24 小时' : '累计';
?>

<section class="hero">
  <?php /* 状态放标题上面：先说「你这台设备怎么样了」，再给标题。
           起始文案中性 —— 探测结果还没出来，不能先替用户说「已经拦下了」。 */ ?>
  <?php if ($logged): ?>
    <div class="hero-state">
      <?php if ($probe_label !== ''): ?>
        <span class="pill pill-wait" data-probe-pill data-probe-label="<?= h($probe_label) ?>">
          <?= icon('info') ?><span data-probe-text>检测中</span>
        </span>
        <?php /* referrerpolicy 不能省：少了它，浏览器会把门户地址放进 Referer
                 头送给探测域名，等于换个方式又泄露一次。 */ ?>
        <img class="probe-img" src="https://<?= h($probe_label . '.' . $probe_host) ?>/p.png"
             width="1" height="1" alt="" referrerpolicy="no-referrer">
      <?php else: ?>
        <span class="pill pill-idle"><?= icon('info') ?>未接入</span>
      <?php endif; ?>
    </div>
  <?php endif; ?>

  <h1 data-probe-head data-probe-when-seen="广告正在被拦下"><?= $logged ? '设备尚未接入' : '加密 DNS 战报' ?></h1>
  <p class="hero-sub">
    <?php if ($logged): ?>
      配置完成后，回到本页即可看到 24 小时查询战报
    <?php else: ?>
      加密 DNS 解析，登录后看自己的用量和日志
    <?php endif; ?>
  </p>
</section>

<section class="card stat-card">
  <div class="stat-label">全站解析总量 · <?= h($scope) ?></div>
  <div class="stat-big" title="<?= h(num_h($q)) ?>"><?= h(num_h($q)) ?></div>
  <div class="stat-lines">
    <div class="stat-line">
      <span class="sl-k">已过滤广告</span>
      <span class="sl-v"><?= h(num_h($b)) ?> 次</span>
    </div>
    <div class="stat-line">
      <span class="sl-k">防误杀放行</span>
      <span class="sl-v"><?= h(num_h($p)) ?> 次</span>
    </div>
    <div class="stat-line">
      <span class="sl-k">拦截率</span>
      <span class="sl-v sl-rate"><?= (int) $rate ?>%</span>
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
    <span class="card-note">峰值 <?= h(num_h(max($points))) ?> 次</span>
  </div>
  <?= linechart($points) ?>
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

<?php if ($logged): ?>
<section class="card">
  <div class="card-head">
    <h2>接入状态</h2>
    <span class="card-note">只判断本设备</span>
  </div>
  <p class="hint">
    这个判断只看一件事：<b>你手上这台设备</b>有没有正在用 <?= h($host) ?> 解析。
    页面上那个随机名字只有这台设备的浏览器知道，所以账号里别的设备、同一个 WiFi
    下的其他设备，都不会被算成这一台。
  </p>
  <p class="hint">
    还没接入的话，去「我的」复制接入地址填进设备的加密 DNS，回到本页就会显示已接入。
  </p>
  <div class="tips">
    <div class="tip">
      <span class="tip-i"><?= icon('info') ?></span>
      <span>路由器上如果开了「DNS 代理 / 上网加速 / 智能选路」，它会覆盖下发的 DNS，设备看起来配好了却仍走运营商解析。先把那一项关掉。</span>
    </div>
  </div>
</section>
<?php endif; ?>

<?php if (!$logged): ?>
<section class="card cta">
  <div>
    <h2>登录查看自己的战报</h2>
    <p class="hint">登录后可以看到自己的用量、接入地址和查询日志。</p>
  </div>
  <a class="btn btn-primary" href="<?= h(page_url('me')) ?>">去登录</a>
</section>
<?php endif; ?>
