<?php
/**
 * 未登录时的首页：公共统计 + 接入方式 + 登录表单。
 */

declare(strict_types=1);
?>
<section class="hero">
  <h1><?= h((string) cfg('title', 'DNS 服务')) ?></h1>
  <p class="sub">加密 DNS 解析服务，支持 DoT 与 DoH。</p>
</section>

<section class="stats">
  <div class="card stat">
    <div class="k">累计解析</div>
    <div class="v"><?= h(num_h((int) ($public['queries'] ?? 0))) ?></div>
  </div>
  <div class="card stat">
    <div class="k">过滤规则</div>
    <div class="v"><?= h(num_h((int) ($public['rules'] ?? 0))) ?></div>
  </div>
  <div class="card stat">
    <div class="k">订阅账号</div>
    <div class="v"><?= h(num_h((int) ($public['accounts'] ?? 0))) ?></div>
  </div>
  <div class="card stat">
    <div class="k">服务状态</div>
    <div class="v <?= !empty($public['protected']) ? 'ok' : 'off' ?>">
      <?= !empty($public['protected']) ? '运行中' : '未防护' ?>
    </div>
  </div>
</section>

<div class="cols">
  <section class="card">
    <h2>接入方式</h2>
    <?php if ($domain !== ''): ?>
      <p class="hint">把下面地址填进你的客户端。域名前缀就是你的账号标识，登录后可以看到自己的。</p>
      <div class="kv">
        <div class="kk">DoT</div>
        <div class="vv"><code>tls://<?= h($domain) ?>:853</code></div>
      </div>
      <div class="kv">
        <div class="kk">DoH</div>
        <div class="vv"><code>https://<?= h($domain) ?>/dns-query</code></div>
      </div>
      <div class="kv">
        <div class="kk">DoQ</div>
        <div class="vv"><code>quic://<?= h($domain) ?>:853</code></div>
      </div>
    <?php else: ?>
      <p class="hint">AGHub 还没配置域名，所以暂时看不到接入地址。</p>
    <?php endif; ?>
  </section>

  <section class="card" id="login">
    <h2>登录</h2>
    <form method="post">
      <input type="hidden" name="action" value="login">
      <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
      <label>
        <span>用户名或邮箱</span>
        <input type="text" name="login" autocomplete="username" required>
      </label>
      <label>
        <span>密码</span>
        <input type="password" name="password" autocomplete="current-password" required>
      </label>
      <button type="submit" class="primary">登录</button>
    </form>
    <?php if (!empty($site['registration_open'])): ?>
      <p class="hint">还没有账号？<a href="<?= h(page_url('register')) ?>">注册一个</a></p>
    <?php endif; ?>
  </section>
</div>
