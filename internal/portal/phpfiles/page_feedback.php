<?php
/**
 * 反馈：给管理员留言。
 */

declare(strict_types=1);
?>
<section class="hero">
  <h1>反馈</h1>
  <p class="hero-sub">用着有问题、想要什么功能，都可以写在这里。</p>
</section>

<?php if (isset($site['announcement']) && $site['announcement'] !== ''): ?>
<section class="notice">
  <?= nl2br(h((string) $site['announcement'])) ?>
</section>
<?php endif; ?>

<?php if (!$logged): ?>
<section class="card cta">
  <div>
    <h2>登录后可以反馈</h2>
    <p class="hint">带上账号才好回你，所以反馈需要先登录。</p>
  </div>
  <a class="btn btn-primary" href="<?= h(page_url('me')) ?>">去登录</a>
</section>
<?php else: ?>
<section class="card">
  <div class="card-head">
    <h2>写点什么</h2>
    <span class="card-note">以 <?= h((string) ($user['name'] ?? '')) ?> 的身份提交</span>
  </div>
  <form method="post">
    <input type="hidden" name="action" value="feedback">
    <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
    <label>
      <span>内容</span>
      <textarea name="content" rows="6" maxlength="2000" required
        placeholder="例如：安卓上私人 DNS 填了之后不生效，路由器是小米 AX3000"></textarea>
    </label>
    <label>
      <span>联系方式（可选）</span>
      <input type="text" name="contact" maxlength="200" placeholder="邮箱 / QQ / TG，方便回你">
    </label>
    <button type="submit" class="btn btn-primary btn-block"><?= icon('send') ?>提交</button>
  </form>
</section>
<?php endif; ?>

<!-- 常见问题不分登录与否，谁都看得到。 -->
<section class="card">
  <div class="card-head"><h2>常见问题</h2></div>
  <div class="faq">
    <div class="faq-item">
      <div class="faq-q">填了地址但没生效？</div>
      <div class="faq-a">路由器上的「DNS 代理 / 上网加速 / 智能选路」会覆盖下发的 DNS，先关掉它再改。</div>
    </div>
    <div class="faq-item">
      <div class="faq-q">手机上怎么填？</div>
      <div class="faq-a">安卓用「私人 DNS」填主机名；iPhone 装描述文件。地址都在「我的」里。</div>
    </div>
    <div class="faq-item">
      <div class="faq-q">战报为什么是空的？</div>
      <div class="faq-a">战报看的是查询记录。设备真的在用这个地址解析之后才会出现数据。</div>
    </div>
    <div class="faq-item">
      <div class="faq-q">会看到我访问了哪些网站吗？</div>
      <div class="faq-a">不会对外显示。日志只给你自己看，别人看不到。</div>
    </div>
  </div>
</section>
