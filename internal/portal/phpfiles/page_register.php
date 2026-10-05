<?php
/**
 * 注册。只在 AGHub 管理端打开注册开关时可达。
 */

declare(strict_types=1);

$email_required = !empty($site['email_required']);
?>
<section class="hero narrow">
  <h1>注册</h1>
  <p class="sub">用户名和邮箱都要填。</p>
</section>

<section class="card narrow">
  <form method="post">
    <input type="hidden" name="action" value="register">
    <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
    <label>
      <span>用户名</span>
      <input type="text" name="name" autocomplete="username" required maxlength="64">
    </label>
    <label>
      <span>邮箱<?= $email_required ? '' : '（可不验证）' ?></span>
      <input type="email" name="email" autocomplete="email" required maxlength="254">
    </label>
    <label>
      <span>密码</span>
      <input type="password" name="password" autocomplete="new-password" required minlength="8">
    </label>
    <button type="submit" class="primary">注册</button>
  </form>
  <p class="hint">已经有账号了？<a href="<?= h(page_url()) ?>">去登录</a></p>
</section>
