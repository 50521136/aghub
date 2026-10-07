<?php
/**
 * 反馈墙：写留言，也看得到别人写了什么。
 *
 * 两栏用排行榜那套标签切换：「大家的」是别人公开出来的留言，「我的」是自己写
 * 的每一条 —— 没勾公开的只出现在「我的」里。默认落在「大家的」：打开这一页
 * 的人多半想先看看别人遇到了什么，而不是先看自己那几条。
 *
 * 公开与否由写的人自己定，管理员事后也能改；所以这里不做任何判断，只按接口
 * 给的 public 渲染。真正决定谁能看到哪条的是 AGHub，不是这一页。
 */

declare(strict_types=1);

$fb_tab = (isset($_GET['tab']) && (string) $_GET['tab'] === 'mine') ? 'mine' : 'all';

// 没登录就没有「我的」这一栏 —— 空标签页只会让人以为自己写过的留言丢了。
if (!$logged) {
    $fb_tab = 'all';
}

/**
 * fb_item 渲染一条留言。
 *
 * 名字只在别人的留言上显示：自己的那几条上面已经有身份了，重复一遍只是噪音。
 * 状态做成小胶囊放在时间旁边，一行读完，不占竖向空间。
 */
$fb_item = function (array $f, bool $show_name): void {
    $resolved = !empty($f['resolved']);
    $public = !empty($f['public']);
    $reply = isset($f['reply']) ? trim((string) $f['reply']) : '';
    $content = isset($f['content']) ? (string) $f['content'] : '';
    $created = isset($f['created_at']) ? (int) $f['created_at'] : 0;
    $replied = isset($f['replied_at']) ? (int) $f['replied_at'] : 0;
    $mine = !empty($f['mine']);
    ?>
    <li class="fb-item">
      <div class="fb-head">
        <?php if ($show_name): ?>
          <span class="fb-who"><?= h((string) ($f['name'] ?? '')) ?></span>
        <?php else: ?>
          <span class="fb-who fb-who-me">我</span>
        <?php endif; ?>
        <span class="fb-time"><?= h(since_h($created)) ?></span>
      </div>

      <p class="fb-text"><?= nl2br(h($content)) ?></p>

      <?php /* 联系方式不在这里显示，也不在接口里：门户拿到的每条留言只有昵称、
             内容和回复，联系方式只到管理员后台。 */ ?>

      <div class="fb-meta">
        <span class="pill <?= $resolved ? 'pill-ok' : 'pill-wait' ?>">
          <?= $resolved ? '已解决' : '待处理' ?>
        </span>
        <span class="pill <?= $public ? 'pill-idle' : 'pill-soft' ?>">
          <?= $public ? '公开' : '未公开' ?>
        </span>
      </div>

      <?php if ($reply !== ''): ?>
        <div class="fb-reply">
          <span class="fb-reply-k">管理员回复</span>
          <p class="fb-text"><?= nl2br(h($reply)) ?></p>
          <?php if ($replied > 0): ?>
            <span class="fb-time"><?= h(since_h($replied)) ?></span>
          <?php endif; ?>
        </div>
      <?php elseif ($mine): ?>
        <p class="fb-wait">还没回复。管理员看过之后会回在这里。</p>
      <?php endif; ?>
    </li>
    <?php
};

$my_count = count($my_feedback);
$other_count = count($other_feedback);
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
<section class="card" id="write">
  <div class="card-head">
    <h2>写点什么</h2>
    <span class="card-note">以 <?= h((string) ($user['name'] ?? '')) ?> 的身份提交</span>
  </div>
  <form method="post">
    <input type="hidden" name="action" value="feedback">
    <input type="hidden" name="csrf" value="<?= h(csrf_token()) ?>">
    <label>
      <span>内容</span>
      <textarea name="content" rows="5" maxlength="2000" required
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

<nav class="rk-tabs">
  <?php if ($logged): ?>
    <a class="rk-tab<?= $fb_tab === 'mine' ? '' : ' on' ?>"
       href="<?= h(page_url('feedback', array('tab' => 'all'))) ?>">大家的<?= $other_count > 0 ? ' ' . $other_count : '' ?></a>
    <a class="rk-tab<?= $fb_tab === 'mine' ? ' on' : '' ?>"
       href="<?= h(page_url('feedback', array('tab' => 'mine'))) ?>">我的<?= $my_count > 0 ? ' ' . $my_count : '' ?></a>
  <?php else: ?>
    <span class="rk-tab on">大家的<?= $other_count > 0 ? ' ' . $other_count : '' ?></span>
  <?php endif; ?>
</nav>

<?php if ($fb_tab === 'mine'): ?>
  <?php if ($my_count === 0): ?>
    <section class="card">
      <div class="fb-empty">
        <?= icon('feedback', 'fb-empty-ic') ?>
        <h2>你还没写过反馈</h2>
        <p class="hint">写一条试试，管理员回复后会出现在这里。</p>
      </div>
    </section>
  <?php else: ?>
    <ul class="fb-list">
      <?php foreach ($my_feedback as $f): ?>
        <?php if (is_array($f)) { $fb_item($f, false); } ?>
      <?php endforeach; ?>
    </ul>
  <?php endif; ?>
<?php else: ?>
  <?php if (!$wall_ok): ?>
    <?php /* 读不到就说读不到，别把整块藏掉 —— 用户看到的是页面缺了一块，只会
             当成坏了。 */ ?>
    <section class="card">
      <p class="hint">读不到反馈列表，稍后再试。</p>
    </section>
  <?php elseif ($other_count === 0): ?>
    <section class="card">
      <div class="fb-empty">
        <?= icon('feedback', 'fb-empty-ic') ?>
        <h2>还没有公开的留言</h2>
        <p class="hint">你可以是第一个。</p>
      </div>
    </section>
  <?php else: ?>
    <ul class="fb-list">
      <?php foreach ($other_feedback as $f): ?>
        <?php if (is_array($f)) { $fb_item($f, true); } ?>
      <?php endforeach; ?>
    </ul>
  <?php endif; ?>
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
    <div class="faq-item">
      <div class="faq-q">反馈会被别人看到吗？</div>
      <div class="faq-a">写的时候勾了「公开」才会出现在「大家的」里，而且只显示你的昵称和留言内容，联系方式不会显示。</div>
    </div>
  </div>
</section>
