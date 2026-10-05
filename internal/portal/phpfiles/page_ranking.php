<?php
/**
 * 排行榜：按累计解析量排的榜单，未登录也能看。
 */

declare(strict_types=1);

$rank_c = cached('ranking', 60, function () {
    return aghub('GET', '/portal/api/ranking?limit=30');
});
$rank_r = $rank_c['v'];
$entries = array();
if (is_array($rank_r) && !empty($rank_r['ok'])) {
    $entries = isset($rank_r['data']['entries']) && is_array($rank_r['data']['entries'])
        ? $rank_r['data']['entries'] : array();
}

// 找自己那一条，好高亮。
$my_ids = $ids;
$my_rank = 0;
$my_entry = array();
foreach ($entries as $e) {
    if (!is_array($e)) {
        continue;
    }

    if (in_array((string) ($e['id'] ?? ''), $my_ids, true)) {
        $my_rank = (int) ($e['rank'] ?? 0);
        $my_entry = $e;
        break;
    }
}

$top = array_slice($entries, 0, 3);
$rest = array_slice($entries, 3);
?>
<section class="hero">
  <h1>排行榜</h1>
  <p class="hero-sub">按累计解析量排的榜，每 60 秒更新一次。</p>
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

<?php if ($top !== array()): ?>
<section class="podium">
  <?php foreach ($top as $k => $e): ?>
    <?php
      $rank = (int) ($e['rank'] ?? ($k + 1));
      $is_me = in_array((string) ($e['id'] ?? ''), $my_ids, true);
      $medal = array(1 => 'gold', 2 => 'silver', 3 => 'bronze');
    ?>
    <div class="podium-item rank-<?= (int) $rank ?><?= $is_me ? ' me' : '' ?>">
      <div class="podium-medal <?= h($medal[$rank] ?? '') ?>"><?= (int) $rank ?></div>
      <div class="podium-name"><?= h((string) ($e['name'] ?? '')) ?></div>
      <div class="podium-num"><?= h(big_h((int) ($e['total_requests'] ?? 0))) ?></div>
      <div class="podium-sub"><?= h((string) ($e['id'] ?? '')) ?></div>
    </div>
  <?php endforeach; ?>
</section>
<?php endif; ?>

<section class="card">
  <div class="card-head">
    <h2>完整榜单</h2>
    <span class="card-note">共 <?= h(num_h(count($entries))) ?> 位</span>
  </div>
  <ol class="rank-list">
    <?php foreach ($rest as $e): ?>
      <?php
        if (!is_array($e)) {
            continue;
        }
        $is_me = in_array((string) ($e['id'] ?? ''), $my_ids, true);
      ?>
      <li class="rank-row<?= $is_me ? ' me' : '' ?>">
        <span class="rank-no"><?= (int) ($e['rank'] ?? 0) ?></span>
        <span class="rank-main">
          <span class="rank-name"><?= h((string) ($e['name'] ?? '')) ?><?= $is_me ? '<em>我</em>' : '' ?></span>
          <span class="rank-id"><?= h((string) ($e['id'] ?? '')) ?></span>
        </span>
        <span class="rank-num"><?= h(big_h((int) ($e['total_requests'] ?? 0))) ?></span>
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
    <p class="hint">榜单按累计解析量排，只显示名字、标识和次数。</p>
  </div>
  <a class="btn btn-primary" href="<?= h(page_url('me')) ?>">去登录</a>
</section>
<?php endif; ?>

<?php endif; ?>
