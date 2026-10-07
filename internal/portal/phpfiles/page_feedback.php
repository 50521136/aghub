<?php
/**
 * 反馈：给管理员留言，也看得到别人写了什么。
 *
 * 两段。上面是「我的反馈」—— 自己写的每条都在这里，没勾公开的也只出现在
 * 这里，管理员回复跟着它；下面是「大家的反馈」—— 别人公开出来的那部分。
 *
 * 公开与否由写的人自己定，管理员事后也能改；所以这里不做任何判断，只按接口
 * 给的 public 渲染。真正决定谁能看到哪条的是 AGHub，不是这一页。
 */

declare(strict_types=1);

/**
 * fb_item 渲染一条留言。
 *
 * 名字只在别人的留言上显示：自己的那几条上面已经有身份了，重复一遍只是噪音。
 */
$fb_item = function (array $f, bool $show_name): void {
    $resolved = !empty($f['resolved']);
    $public = !empty($f['public']);
    $reply = isset($f['reply']) ? trim((string) $f['reply']) : '';
    $content = isset($f['content']) ? (string) $f['content'] : '';
    $created = isset($f['created_at']) ? (int) $f['created_at'] : 0;
    $replied = isset($f['replied_at']) ? (int) $f['replied_at'] : 0;
    ?>
    <li class="fb-item">
      <div class="fb-head">
        <?php if ($show_name): ?>
          <span class="fb-who"><?= h((string) ($f['name'] ?? '')) ?></span>
        <?php endif; ?>
        <span class="pill <?= $resolved ? 'pill-ok' : 'pill-wait' ?>">
          <?= $resolved ? '已解决' : '待处理' ?>
        </span>
        <span class="pill <?= $public ? 'pill-idle' : 'pill-soft' ?>">
          <?= $public ? '公开' : '未公开' ?>
        </span>
        <span class="fb-time"><?= h(since_h($created)) ?></span>
      </div>

      <p class="fb-text"><?= h($content) ?></p>

      <?php if ($reply !== ''): ?>
        <div class="fb-reply">
          <span class="fb-reply-k">管理员回复</span>
          <p class="fb-text"><?= h($reply) ?></p>
          <?php if ($replied > 0): ?>
            <span class="fb-time"><?= h(since_h($replied)) ?></span>
          <?php endif; ?>
        </div>
      <?php else: ?>
        <p class="hint">还没回复。回复之后会显示在这里。</p>
      <?php endif; ?>
    </li>
    <?php
};
?>
<section class="hero">
  <h1>反馈</h1>
  <p class="hero-sub">用着有问题、想要什么功能，都可以写在这里。管理员回复后会出现在下面。</p>
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
    <?php /* 勾选框默认勾上：多数反馈对别人也有用，而且这样才有回复的价值。
             没勾的话只有自己和管理员看得到，联系方式也不会跟着留言显示出去。 */ ?>
    <label class="check-row">
      <input type="checkbox" name="public" value="1" checked>
      <span>公开这条反馈，让别人也看得到（会显示你的昵称，不显示联系方式）</span>
    </label>
    <button type="submit" class="btn btn-primary btn-block"><?= icon('send') ?>提交</button>
  </form>
</section>
<?php endif; ?>

<?php if ($logged): ?>
<section class="card">
  <div class="card-head">
    <h2>我的反馈</h2>
    <span class="card-note"><?= $my_feedback === array() ? '还没写过' : count($my_feedback) . ' 条' ?></span>
  </div>
  <?php if ($my_feedback === array()): ?>
    <p class="hint">写一条试试，管理员回复后会出现在这里。</p>
  <?php else: ?>
    <ul class="fb-list">
      <?php foreach ($my_feedback as $f): ?>
        <?php if (is_array($f)) { $fb_item($f, false); } ?>
      <?php endforeach; ?>
    </ul>
  <?php endif; ?>
</section>
<?php endif; ?>

<section class="card">
  <div class="card-head">
    <h2>大家的反馈</h2>
    <span class="card-note">公开的留言</span>
  </div>

  <?php if (!$wall_ok): ?>
    <?php /* 读不到就说读不到，别把整块藏掉 —— 用户看到的是页面缺了一块，只会
             当成坏了。 */ ?>
    <p class="hint">读不到反馈列表，稍后再试。</p>
  <?php elseif ($other_feedback === array()): ?>
    <p class="hint">还没有别人公开的留言。你可以是第一个。</p>
  <?php else: ?>
    <ul class="fb-list">
      <?php foreach ($other_feedback as $f): ?>
        <?php if (is_array($f)) { $fb_item($f, true); } ?>
      <?php endforeach; ?>
    </ul>
  <?php endif; ?>
</section>

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
    <div class="faq-item">
      <div class="faq-q">反馈会被别人看到吗？</div>
      <div class="faq-a">写的时候勾了「公开」才会出现在上面，而且只显示你的昵称和留言内容，联系方式不会显示。</div>
    </div>
  </div>
</section>
