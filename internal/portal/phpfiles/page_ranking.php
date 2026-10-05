<?php
/**
 * 排行榜：按累计解析量排的榜单，未登录也能看。
 *
 * 用列表呈现，不做领奖台 —— 手机上列表更好扫，也不用为前三名单独排一套版式。
 */

declare(strict_types=1);

// 榜单在 index.php 里已经跟其它接口一起并发取回来了。

// 找自己那一条，好高亮。
$my_ids = $ids;
$my_rank = 0;
$my_entry = array();
$peak = 0;
foreach ($entries as $e) {
    if (!is_array($e)) {
        continue;
    }

    $n = (int) ($e['total_requests'] ?? 0);
    if ($n > $peak) {
        $peak = $n;
    }

    if ($my_rank === 0 && in_array((string) ($e['id'] ?? ''), $my_ids, true)) {
        $my_rank = (int) ($e['rank'] ?? 0);
        $my_entry = $e;
    }
}
?>
<section class="hero">
  <h1>排行榜</h1>
  <p class="hero-sub">按累计解析量排，每 60 秒更新。</p>
</section>

<?php if ($entries === array()): ?>
  <section class="card">
    <p class="hint">
      <?php if (is_array($rank_r) && empty($rank_r['ok'])): ?>
        读不到榜单。<?= h((string) $rank_r['error']) ?>
      <?php else: ?>
        还没有人上榜。用起来就会出现在这里。
      <?php endif; ?>
    </p>
  </section>
<?php else: ?>

<section class="card rank-card">
  <div class="card-head">
    <h2>完整榜单</h2>
    <span class="card-note">共 <?= h(num_h(count($entries))) ?> 位</span>
  </div>

  <ol class="rank-list">
    <?php foreach ($entries as $e): ?>
      <?php
        if (!is_array($e)) {
            continue;
        }

        $rank = (int) ($e['rank'] ?? 0);
        $is_me = in_array((string) ($e['id'] ?? ''), $my_ids, true);
        $n = (int) ($e['total_requests'] ?? 0);
        $w = $peak > 0 ? (int) round($n * 100 / $peak) : 0;
        if ($w < 2 && $n > 0) {
            $w = 2;
        }
      ?>
      <li class="rank-row<?= $is_me ? ' me' : '' ?>">
        <span class="rank-no<?= $rank <= 3 ? ' top' : '' ?>"><?= $rank ?></span>
        <span class="rank-main">
          <span class="rank-name"><?= h((string) ($e['name'] ?? '')) ?><?= $is_me ? '<em>我</em>' : '' ?></span>
          <span class="rank-bar"><i style="width:<?= $w ?>%"></i></span>
        </span>
        <span class="rank-num"><?= h(big_h($n)) ?></span>
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
      <p class="hint">榜单只收有解析记录的账号，用起来就会上去。</p>
    </div>
  <?php endif; ?>
  <a class="btn" href="<?= h(page_url('log')) ?>">看我的日志</a>
</section>
<?php else: ?>
<section class="card cta">
  <div>
    <h2>登录查看自己的排名</h2>
    <p class="hint">榜单只显示名字和次数。</p>
  </div>
  <a class="btn btn-primary" href="<?= h(page_url('me')) ?>">去登录</a>
</section>
<?php endif; ?>

<?php endif; ?>
