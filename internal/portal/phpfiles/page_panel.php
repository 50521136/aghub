<?php
/**
 * 登录后的概览：账号状态、配额、有效期、专属接入地址。
 */

declare(strict_types=1);

$limit = (int) ($user['request_limit'] ?? 0);
$used = (int) ($user['requests'] ?? 0);
$remaining = (int) ($user['remaining_requests'] ?? 0);
$status = (string) ($user['status'] ?? '');
$unlimited = $limit <= 0;
$p = $unlimited ? 0 : pct($used, $limit);
$bar_class = $p >= 90 ? 'danger' : ($p >= 70 ? 'warn' : '');

$client = isset($user['client']) && is_array($user['client']) ? $user['client'] : array();
$connected = !empty($client['connected']);
$client_ip = (string) ($client['ip'] ?? '');
$client_device = (string) ($client['device'] ?? '');
?>
<section class="hello">
  <div>
    <h1><?= h((string) ($user['name'] ?? '')) ?></h1>
    <p class="sub">
      标识 <code><?= h($primary_id !== '' ? $primary_id : '—') ?></code>
      <span class="dot">·</span>
      状态 <span class="pill <?= $status === 'active' ? 'good' : 'bad' ?>"><?= h(status_h($status)) ?></span>
    </p>
  </div>
  <a class="btn" href="<?= h(page_url('log')) ?>">查询日志</a>
</section>

<section class="stats">
  <div class="card stat">
    <div class="k">本<?= h(str_replace('每', '', period_h((string) ($user['period'] ?? 'day')))) ?>已用</div>
    <div class="v"><?= h(num_h($used)) ?></div>
    <?php if (!$unlimited): ?>
      <div class="bar"><i class="<?= h($bar_class) ?>" style="width:<?= (int) $p ?>%"></i></div>
      <div class="sub2">上限 <?= h(num_h($limit)) ?>，还剩 <?= h(num_h($remaining)) ?></div>
    <?php else: ?>
      <div class="sub2">不限量</div>
    <?php endif; ?>
  </div>
  <div class="card stat">
    <div class="k">累计解析</div>
    <div class="v"><?= h(num_h((int) ($user['total_requests'] ?? 0))) ?></div>
    <div class="sub2">这个账号从建立到现在</div>
  </div>
  <div class="card stat">
    <div class="k">配额重置</div>
    <div class="v small"><?= h(when_h((int) ($user['next_reset'] ?? 0))) ?></div>
    <div class="sub2"><?= h(period_h((string) ($user['period'] ?? 'day'))) ?>重置一次</div>
  </div>
  <div class="card stat">
    <div class="k">有效期</div>
    <div class="v small"><?= h(remaining_h((int) ($user['remaining_seconds'] ?? -1))) ?></div>
    <div class="sub2"><?= (int) ($user['expires_at'] ?? 0) > 0 ? h(when_h((int) $user['expires_at']) . ' 到期') : '长期有效' ?></div>
  </div>
</section>

<div class="cols">
  <section class="card">
    <h2>我的接入地址</h2>
    <?php if ($primary_id !== '' && $domain !== ''): ?>
      <p class="hint">域名前缀是你的专属标识，别人用不了。填进客户端即可。</p>
      <div class="kv">
        <div class="kk">DoT</div>
        <div class="vv"><code>tls://<?= h(dot_host($primary_id, $domain)) ?>:853</code></div>
      </div>
      <div class="kv">
        <div class="kk">DoH</div>
        <div class="vv"><code><?= h(doh_url($primary_id, $domain)) ?></code></div>
      </div>
      <div class="kv">
        <div class="kk">DoQ</div>
        <div class="vv"><code>quic://<?= h(dot_host($primary_id, $domain)) ?>:853</code></div>
      </div>
    <?php else: ?>
      <p class="hint">管理员还没有给你分配标识，或者 AGHub 还没配置域名。</p>
    <?php endif; ?>
  </section>

  <section class="card">
    <h2>连接情况</h2>
    <div class="kv">
      <div class="kk">最近活动</div>
      <div class="vv"><?= h(since_h((int) ($user['last_seen'] ?? 0))) ?></div>
    </div>
    <div class="kv">
      <div class="kk">来源地址</div>
      <div class="vv"><?= $client_ip !== '' ? h($client_ip) : '—' ?></div>
    </div>
    <?php if ($client_device !== ''): ?>
      <div class="kv">
        <div class="kk">设备</div>
        <div class="vv"><?= h($client_device) ?></div>
      </div>
    <?php endif; ?>
    <div class="kv">
      <div class="kk">状态</div>
      <div class="vv">
        <span class="pill <?= $connected ? 'good' : '' ?>"><?= $connected ? '在线' : '未检测到' ?></span>
      </div>
    </div>
    <p class="hint">解析一次就会更新，不用手动刷新。</p>
  </section>
</div>
