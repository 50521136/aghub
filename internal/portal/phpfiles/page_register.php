<?php
/**
 * 注册。只在 AGHub 管理端打开注册开关时可达。
 */

declare(strict_types=1);

$email_required = !empty($site['email_required']);
?>
<section class="hero">
  <h1>注册</h1>
  <p class="hero-sub">用户名和邮箱都要填<?= $email_required ? '，邮箱需要验证' : '，邮箱暂不验证' ?>。</p>
</section>

<section class="card">
  <form method="post">
    <input type="hidden" name="action" value="register">
    <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
    <label>
      <span>用户名</span>
      <input type="text" name="name" autocomplete="username" required maxlength="64" autofocus>
    </label>
    <label>
      <span>邮箱</span>
      <input type="email" name="email" autocomplete="email" required maxlength="254">
    </label>
    <label>
      <span>密码（至少 8 位）</span>
      <input type="password" name="password" autocomplete="new-password" required minlength="8">
    </label>
    <button type="submit" class="btn btn-primary btn-block">注册</button>
  </form>
  <p class="hint">已经有账号了？<a class="link" href="<?= h(page_url('me')) ?>">去登录</a></p>
</section>
