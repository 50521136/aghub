<?php
/**
 * 排行榜：两个口径，今日和累计，默认今日。
 *
 * 今日榜从北京时间 0 点起算，回答「现在谁在用」；累计榜回答「谁一直在用」。
 * 默认给前者 —— 累计榜的名次几天都不动，新来的人在上面看不到自己。
 *
 * 卡片式：每张卡是一个人，名次、头像、名字一行，解析量做进度条，下面跟
 * 拦截、防误杀、平均延迟三格。比纯列表多花一点竖向空间，但手机上拇指
 * 扫过去能直接读出「谁解析得多、谁拦得多」。
 *
 * 三格数字必须和解析量同一个口径：今日榜上就该是今天的拦截量和今天的
 * 防误杀。曾经它们取的是累计值，于是今日榜的一张卡上会同时出现
 * 「今日 14983 次解析」和「拦截 9478 + 防误杀 17533」—— 两个过滤数字加
 * 起来比今日总量还大。口径由后端按榜单窗口给（blocked/passed 两个字段
 * 跟着 order 切换），页面不自己算。
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
          <?php
            // 拦截率 = 拦截 / (拦截 + 防误杀)，也就是「命中过滤规则的查询里
            // 有多少被真正拦掉」。只看绝对量的话，一个用量大的人拦截量天然
            // 就高，比不出谁的白名单更准。
            //
            // 两个计数都必须非零才显示：任一侧为 0 时百分比必然是 0% 或
            // 100%，不带信息量，而且分不清「确实一条都没有」和「这个窗口
            // 刚开始统计」—— 后者在升级/重启后的头一天是常态（小时环里
            // 早先的桶没有过滤计数）。曾经这里只判断总和非零，结果升级后
            // 满屏都是「拦截 100%」。
            $blk0 = max(0, $blk);
            $pas0 = max(0, $pas);
            $hits = $blk0 + $pas0;
            $rate = ($blk0 > 0 && $pas0 > 0) ? (int) round($blk0 * 100 / $hits) : -1;
          ?>
          <?php if ($blk >= 0 || $pas >= 0): ?>
            <span class="rk-metrics">
              <span class="rk-m blk">
                <b><?= h(big_h($blk0)) ?></b>
                <span>拦截<?= $rate >= 0 ? ' ' . $rate . '%' : '' ?></span>
              </span>
              <span class="rk-m pas">
                <b><?= h(big_h($pas0)) ?></b>
                <span>防误杀</span>
              </span>
              <span class="rk-m">
                <b><?= h(ms_h($lat, $lat_n)) ?></b>
                <span>平均延迟</span>
              </span>
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
