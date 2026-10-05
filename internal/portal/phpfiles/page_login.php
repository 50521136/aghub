<?php
/**
 * 登录表单。未登录时「我的」显示的就是它。
 */

declare(strict_types=1);
?>
<section class="hero">
  <h1>登录</h1>
  <p class="hero-sub">登录后可以看到自己的专属标识、接入地址和查询日志。</p>
</section>

<section class="card">
  <form method="post">
    <input type="hidden" name="action" value="login">
    <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
    <label>
      <span>用户名或邮箱</span>
      <input type="text" name="login" autocomplete="username" required autofocus>
    </label>
    <label>
      <span>密码</span>
      <input type="password" name="password" autocomplete="current-password" required>
    </label>
    <button type="submit" class="btn btn-primary btn-block">登录</button>
  </form>
  <?php if (!empty($site['registration_open'])): ?>
    <p class="hint">还没有账号？<a class="link" href="<?= h(page_url('register')) ?>">注册一个</a></p>
  <?php else: ?>
    <p class="hint">账号由管理员开通，忘了密码找管理员重置。</p>
  <?php endif; ?>
</section>

<section class="card">
  <div class="card-head"><h2>没登录也能看</h2></div>
  <div class="quick">
    <a class="quick-item" href="<?= h(page_url('report')) ?>"><?= icon('chart') ?><span>全站战报</span></a>
    <a class="quick-item" href="<?= h(page_url('ranking')) ?>"><?= icon('trophy') ?><span>排行榜</span></a>
    <a class="quick-item" href="<?= h(page_url('feedback')) ?>"><?= icon('feedback') ?><span>反馈</span></a>
  </div>
</section>
