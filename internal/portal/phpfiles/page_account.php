<?php
/**
 * 账号信息与修改密码。
 */

declare(strict_types=1);

$all_ids = isset($user['ids']) && is_array($user['ids']) ? $user['ids'] : array();
?>
<section class="hello">
  <div>
    <h1>账号</h1>
    <p class="sub"><?= h((string) ($user['name'] ?? '')) ?></p>
  </div>
</section>

<div class="cols">
  <section class="card">
    <h2>基本信息</h2>
    <div class="kv">
      <div class="kk">用户名</div>
      <div class="vv"><?= h((string) ($user['name'] ?? '—')) ?></div>
    </div>
    <div class="kv">
      <div class="kk">标识</div>
      <div class="vv">
        <?php if ($all_ids === array()): ?>
          —
        <?php else: ?>
          <?php foreach ($all_ids as $i => $one): ?>
            <?= $i > 0 ? '、' : '' ?><code><?= h((string) $one) ?></code>
          <?php endforeach; ?>
        <?php endif; ?>
      </div>
    </div>
    <div class="kv">
      <div class="kk">建立时间</div>
      <div class="vv"><?= h(when_h((int) ($user['created_at'] ?? 0))) ?></div>
    </div>
    <div class="kv">
      <div class="kk">配额</div>
      <div class="vv">
        <?php if ((int) ($user['request_limit'] ?? 0) <= 0): ?>
          不限量
        <?php else: ?>
          <?= h(num_h((int) $user['request_limit'])) ?> / <?= h(period_h((string) ($user['period'] ?? 'day'))) ?>
        <?php endif; ?>
      </div>
    </div>
    <div class="kv">
      <div class="kk">到期</div>
      <div class="vv">
        <?= (int) ($user['expires_at'] ?? 0) > 0 ? h(when_h((int) $user['expires_at'])) : '长期有效' ?>
      </div>
    </div>
  </section>

  <section class="card">
    <h2>修改密码</h2>
    <form method="post">
      <input type="hidden" name="action" value="password">
      <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
      <label>
        <span>当前密码</span>
        <input type="password" name="old_password" autocomplete="current-password" required>
      </label>
      <label>
        <span>新密码</span>
        <input type="password" name="new_password" autocomplete="new-password" required minlength="8">
      </label>
      <label>
        <span>再输一次</span>
        <input type="password" name="new_password2" autocomplete="new-password" required minlength="8">
      </label>
      <button type="submit" class="primary">保存</button>
    </form>
    <p class="hint">至少 8 位。改完不用重新登录。</p>
  </section>
</div>
