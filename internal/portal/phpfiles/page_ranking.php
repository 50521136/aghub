<?php
/**
 * 排行榜：按累计解析量排的榜单，未登录也能看。
 *
 * 卡片式：每张卡是一个人，名次、头像、名字一行，解析量做进度条，下面跟
 * 拦截量和防误杀。比纯列表多花一点竖向空间，但手机上拇指扫过去能直接
 * 读出「谁解析得多、谁拦得多」。
 */

declare(strict_types=1);

// 榜单在 index.php 里已经跟其它接口一起并发取回来了。

// 找自己那一条，好高亮。
$my_ids = $ids;
$my_rank = 0;
$my_entry = array();
$peak = 0;
$shown = 0;
foreach ($entries as $e) {
    if (!is_array($e)) {
        continue;
    }

    $n = (int) ($e['total_requests'] ?? 0);
    if ($n > $peak) {
        $peak = $n;
    }

    $shown++;

    if ($my_rank === 0 && in_array((string) ($e['id'] ?? ''), $my_ids, true)) {
        $my_rank = (int) ($e['rank'] ?? 0);
        $my_entry = $e;
    }
}
?>
<section class="hero">
  <h1>排行榜</h1>
  <p class="hero-sub">解析量超过 1000 才上榜，每 60 秒更新。</p>
</section>

<?php if ($shown === 0): ?>
  <section class="card">
    <p class="hint">
      <?php if (is_array($rank_r) && empty($rank_r['ok'])): ?>
        读不到榜单。<?= h((string) $rank_r['error']) ?>
      <?php else: ?>
        还没有人上榜。解析量超过 1000 就会出现在这里。
      <?php endif; ?>
    </p>
  </section>
<?php else: ?>

<section class="rk-wrap">
  <ol class="rk-list">
    <?php foreach ($entries as $e): ?>
      <?php
        if (!is_array($e)) {
            continue;
        }

        $rank = (int) ($e['rank'] ?? 0);
        $is_me = in_array((string) ($e['id'] ?? ''), $my_ids, true);
        $n = (int) ($e['total_requests'] ?? 0);
        $blk = isset($e['blocked']) ? (int) $e['blocked'] : -1;
        $pas = isset($e['passed']) ? (int) $e['passed'] : -1;

        $w = $peak > 0 ? (int) round($n * 100 / $peak) : 0;
        if ($w < 3 && $n > 0) {
            $w = 3;
        }

        $name = (string) ($e['name'] ?? '');
        $ava = (string) ($e['avatar'] ?? '');
      ?>
      <li class="rk-item<?= $is_me ? ' me' : '' ?>">
        <span class="rk-no<?= $rank <= 3 ? ' top' : '' ?>"><?= $rank ?></span>
        <?= avatar_html($ava, $name, (string) ($e['id'] ?? '')) ?>
        <span class="rk-body">
          <span class="rk-line">
            <span class="rk-name"><?= h($name) ?><?= $is_me ? '<em>我</em>' : '' ?></span>
            <span class="rk-total" title="<?= h(num_h($n)) ?>"><?= h(num_h($n)) ?><small>解析量</small></span>
          </span>
          <span class="rk-bar"><i style="width:<?= $w ?>%"></i></span>
          <?php if ($blk >= 0 || $pas >= 0): ?>
            <span class="rk-stats">
              <span><b><?= h(num_h(max(0, $blk))) ?></b>拦截量</span>
              <span><b><?= h(num_h(max(0, $pas))) ?></b>防误杀</span>
            </span>
          <?php endif; ?>
        </span>
      </li>
    <?php endforeach; ?>
  </ol>
</section>

<?php if ($logged): ?>
<section class="card cta">
  <?php if ($my_rank > 0): ?>
    <div>
      <h2>你在第 <?= (int) $my_rank ?> 名</h2>
      <p class="hint">累计 <?= h(num_h((int) ($my_entry['total_requests'] ?? 0))) ?> 次解析。</p>
    </div>
  <?php else: ?>
    <div>
      <h2>你还没上榜</h2>
      <p class="hint">解析量超过 1000 就会上去，用起来就行。</p>
    </div>
  <?php endif; ?>
  <a class="btn" href="<?= h(page_url('log')) ?>">看我的日志</a>
</section>
<?php else: ?>
<section class="card cta">
  <div>
    <h2>登录查看自己的排名</h2>
    <p class="hint">榜单只显示名字和数量。</p>
  </div>
  <a class="btn btn-primary" href="<?= h(page_url('me')) ?>">去登录</a>
</section>
<?php endif; ?>

<?php endif; ?>
