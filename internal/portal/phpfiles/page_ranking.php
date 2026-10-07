<?php
/**
 * 排行榜：两个口径，今日和累计，默认今日。
 *
 * 今日榜从北京时间 0 点起算，回答「现在谁在用」；累计榜回答「谁一直在用」。
 * 默认给前者 —— 累计榜的名次几天都不动，新来的人在上面看不到自己。
 *
 * 卡片式：每张卡是一个人，名次、头像、名字一行，解析量做进度条，下面跟
 * 拦截量和防误杀。比纯列表多花一点竖向空间，但手机上拇指扫过去能直接
 * 读出「谁解析得多、谁拦得多」。
 */

declare(strict_types=1);

// 榜单在 index.php 里已经跟其它接口一起并发取回来了。

$is_today = $rank_order !== 'total';

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

    $n = rank_figure($e, $rank_order);
    if ($n > $peak) {
        $peak = $n;
    }

    $shown++;

    if ($my_rank === 0 && in_array((string) ($e['id'] ?? ''), $my_ids, true)) {
        $my_rank = (int) ($e['rank'] ?? 0);
        $my_entry = $e;
    }
}

$scope = $is_today ? '今日' : '累计';
?>
<section class="hero">
  <h1>排行榜</h1>
  <p class="hero-sub">解析量超过 1000 才上榜，每 60 秒更新。</p>
</section>

<nav class="rk-tabs">
  <a class="rk-tab<?= $is_today ? ' on' : '' ?>" href="<?= h(page_url('ranking', array('order' => 'today'))) ?>">今日</a>
  <a class="rk-tab<?= $is_today ? '' : ' on' ?>" href="<?= h(page_url('ranking', array('order' => 'total'))) ?>">累计</a>
</nav>

<?php if ($shown === 0): ?>
  <section class="card">
    <div class="card-head">
      <h2>还没有人上榜</h2>
      <span class="card-note">解析量 > 1000</span>
    </div>
    <p class="hint">
      <?php if (!is_array($rank_r) || !isset($rank_r['ok']) || empty($rank_r['ok'])): ?>
        <?php /* 三种情况都要说清楚，而且不能去访问不存在的键 —— 之前这里
                 直接读 $rank_r['error']，取不到数据时页面会蹦一条 PHP 警告。 */ ?>
        读不到榜单。<?= h(isset($rank_r['error']) && $rank_r['error'] !== ''
            ? (string) $rank_r['error'] : '稍后再试。') ?>
      <?php elseif ($is_today): ?>
        今天还没有解析量。设备接进来用一会儿，这里就会出现。
      <?php else: ?>
        榜单按累计解析量排，超过 1000 才会出现。设备接进来用一阵子就会上去。
      <?php endif; ?>
    </p>
    <?php if ($logged && $primary_id !== ''): ?>
      <p class="hint">
        你在榜上的进度看「我的」里的累计解析量。
      </p>
    <?php endif; ?>
  </section>
<?php else: ?>

<section class="rk-wrap">
  <div class="rk-note">
    <span>按<?= h($scope) ?>解析量排序</span>
    <span class="rk-rule">解析量 > 1000 上榜</span>
  </div>
  <ol class="rk-list">
    <?php foreach ($entries as $e): ?>
      <?php
        if (!is_array($e)) {
            continue;
        }

        $rank = (int) ($e['rank'] ?? 0);
        $is_me = in_array((string) ($e['id'] ?? ''), $my_ids, true);
        $n = rank_figure($e, $rank_order);
        $blk = isset($e['blocked']) ? (int) $e['blocked'] : -1;
        $pas = isset($e['passed']) ? (int) $e['passed'] : -1;

        // 平均延迟跟着榜单口径走：今日榜给今天的，累计榜给开户以来的。
        // 没有样本的（新账号、还没解析过）显示破折号，不显示 0。
        $lat = isset($e['avg_latency_ms']) ? (float) $e['avg_latency_ms'] : 0.0;
        $lat_n = isset($e['latency_samples']) ? (int) $e['latency_samples'] : 0;

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
            <span class="rk-total" title="<?= h(num_h($n)) ?>"><?= h(num_h($n)) ?><small><?= h($scope) ?></small></span>
          </span>
          <span class="rk-bar"><i style="width:<?= $w ?>%"></i></span>
          <?php if ($blk >= 0 || $pas >= 0): ?>
            <span class="rk-stats">
              <span><b><?= h(num_h(max(0, $blk))) ?></b>拦截量</span>
              <span><b><?= h(num_h(max(0, $pas))) ?></b>防误杀</span>
              <span><b><?= h(ms_h($lat, $lat_n)) ?></b>平均延迟</span>
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
      <p class="hint">
        <?= h($scope) ?> <?= h(num_h(rank_figure($my_entry, $rank_order))) ?> 次解析，
        平均延迟 <?= h(ms_h(
            isset($my_entry['avg_latency_ms']) ? (float) $my_entry['avg_latency_ms'] : 0.0,
            isset($my_entry['latency_samples']) ? (int) $my_entry['latency_samples'] : 0,
        )) ?>。
      </p>
    </div>
  <?php else: ?>
    <div>
      <h2>你还没上榜</h2>
      <p class="hint">
        <?php if ($is_today): ?>
          今天还没有你的解析量，设备用一会儿就会上去。
        <?php else: ?>
          解析量超过 1000 就会上去，用起来就行。
        <?php endif; ?>
      </p>
    </div>
  <?php endif; ?>
  <a class="btn" href="<?= h(page_url('log')) ?>">看我的日志</a>
</section>

<?php /* 榜单和反馈是同一件事的两面：看到别人的数据，再问一句为什么。所以从
         这里留一个入口，不用退回导航再点一次。 */ ?>
<p class="hint rk-more">
  用着不顺手、想加点什么？<a href="<?= h(page_url('feedback')) ?>">去反馈墙说一句</a>。
</p>
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
