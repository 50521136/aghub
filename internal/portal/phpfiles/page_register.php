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
    <?php if ($email_required): ?>
      <?php /* 验证码只在开关打开时出现。发送按钮走本站的 ?p=emailcode ——
               浏览器不直接找 AGHub，这是门户一直以来的规矩。 */ ?>
      <label>
        <span>邮箱验证码</span>
        <span class="field-row">
          <input type="text" name="code" inputmode="numeric" autocomplete="one-time-code"
                 maxlength="6" pattern="[0-9]{6}" placeholder="6 位数字" required>
          <button type="button" class="btn" data-code-btn
                  data-csrf="<?= h(csrf_token()) ?>">获取验证码</button>
        </span>
      </label>
      <p class="hint" data-code-hint>点「获取验证码」，邮件里会有 6 位数字，15 分钟内有效。</p>
    <?php endif; ?>
    <label>
      <span>密码（至少 8 位）</span>
      <input type="password" name="password" autocomplete="new-password" required minlength="8">
    </label>
    <button type="submit" class="btn btn-primary btn-block">注册</button>
  </form>
  <p class="hint">已经有账号了？<a class="link" href="<?= h(page_url('me')) ?>">去登录</a></p>
</section>
