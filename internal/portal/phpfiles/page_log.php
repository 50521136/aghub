<?php
/**
 * 查询日志。只显示自己的请求。
 *
 * 这一页是「翻来覆去看」的典型，所以结果缓存 30 秒：切来切去不会再打 AGHub
 * 一次。要最新的点「刷新」，会跳过缓存重新取。
 *
 * 日志要连续签到够了才开 —— 它是唯一能看到设备在问什么的页面，也正是天天
 * 来签到的回报。没开的时候说清楚还差几天，不要假装「没有记录」。
 */

declare(strict_types=1);

$term = isset($_GET['term']) ? trim((string) $_GET['term']) : '';
$limit = isset($_GET['limit']) ? (int) $_GET['limit'] : 50;
if ($limit < 20 || $limit > 200) {
    $limit = 50;
}

$count = count($log);
$age = $log_at > 0 ? max(0, time() - $log_at) : 0;

$lk_streak = (int) ($checkin['streak'] ?? 0);
$lk_need = (int) ($checkin['log_unlock_streak'] ?? 0);
$lk_daily = (int) ($checkin['daily_bonus'] ?? 0);
?>

<?php if ($log_locked): ?>
<section class="hero">
  <h1>查询日志</h1>
  <p class="hero-sub">连续签到 <?= (int) $lk_need ?> 天开启。</p>
</section>

<section class="card">
  <div class="card-head">
    <h2>还差 <?= (int) max(0, $lk_need - $lk_streak) ?> 天</h2>
    <span class="card-note">现在连续 <?= (int) $lk_streak ?> 天</span>
  </div>
  <p class="hint">
    日志是唯一能看到设备在问什么的页面，所以它跟着签到走：连续签到
    <b><?= (int) $lk_need ?> 天</b>就打开，之后一直有效 —— 断了一天不会收回去。
  </p>
  <?php if ($lk_daily > 0): ?>
    <p class="hint">每天签到还会临时多 <?= h(num_h($lk_daily)) ?> 次解析额度，当天有效。</p>
  <?php endif; ?>
  <a class="btn btn-primary btn-wide" href="<?= h(page_url()) ?>">去签到</a>
</section>
<?php else: ?>

<section class="hero">
  <div class="hero-row">
    <div>
      <h1>查询日志</h1>
      <p class="hero-sub">最近 <?= (int) $count ?> 条，只显示你自己的请求。</p>
    </div>
    <a class="btn btn-icon" href="<?= h(page_url('log', array('limit' => $limit, 'fresh' => 1))) ?>"
       title="跳过缓存重新取"><?= icon('refresh') ?>刷新</a>
  </div>
</section>

<form class="search" method="get">
  <input type="hidden" name="p" value="log">
  <input type="hidden" name="limit" value="<?= (int) $limit ?>">
  <input type="text" name="term" value="<?= h($term) ?>" placeholder="搜域名、客户端或地址">
  <button type="submit" class="btn">搜索</button>
</form>

<div class="log-meta">
  <?php if ($log_fresh): ?>
    <span class="tag tag-ok"><?= icon('check') ?>刚刚获取</span>
  <?php else: ?>
    <span class="tag"><?= icon('clock') ?><?= (int) $age ?> 秒前的缓存</span>
  <?php endif; ?>
  <span class="log-meta-txt">30 秒内翻回本页不会再请求一次</span>
  <span class="row-actions">
    <a class="link" href="<?= h(page_url('log', array('limit' => 20, 'term' => $term))) ?>">20 条</a>
    <a class="link<?= $limit === 50 ? ' on' : '' ?>" href="<?= h(page_url('log', array('limit' => 50, 'term' => $term))) ?>">50 条</a>
    <a class="link<?= $limit === 100 ? ' on' : '' ?>" href="<?= h(page_url('log', array('limit' => 100, 'term' => $term))) ?>">100 条</a>
  </span>
</div>

<?php if ($log === array()): ?>
<section class="card">
  <p class="hint">
    <?php if ($term !== ''): ?>
      没有匹配「<?= h($term) ?>」的记录。
    <?php else: ?>
      还没有记录。设备用你的专属地址解析之后，这里就会出现。
    <?php endif; ?>
  </p>
</section>
<?php else: ?>
<section class="log-list">
  <?php foreach ($log as $e): ?>
    <?php
      if (!is_array($e)) {
          continue;
      }

      $q = isset($e['question']) && is_array($e['question']) ? $e['question'] : array();
      $name = isset($q['name']) ? rtrim((string) $q['name'], '.') : '';
      $qtype = isset($q['type']) ? (string) $q['type'] : '';
      $client = isset($e['client']) ? (string) $e['client'] : '';
      $elapsed = isset($e['elapsedMs']) ? (string) $e['elapsedMs'] : '';
      $cached_hit = !empty($e['cached']);
      $ts = isset($e['time']) ? strtotime((string) $e['time']) : false;

      // 结果压成一行
      $answer = isset($e['answer']) && is_array($e['answer']) ? $e['answer'] : array();
      $parts = array();
      foreach (array_slice($answer, 0, 2) as $a) {
          if (is_array($a) && isset($a['value'])) {
              $parts[] = (string) $a['value'];
          }
      }
      $ans = $parts === array() ? '' : implode('，', $parts);
      if (count($answer) > 2) {
          $ans .= ' 等 ' . count($answer) . ' 条';
      }

      // 上游
      $up = '';
      if (isset($e['upstream']) && is_string($e['upstream']) && $e['upstream'] !== '') {
          $up = (string) $e['upstream'];
      }

      $blocked = ($qtype === '' && $ans === '') || $ans === '0.0.0.0' || $ans === '::';
    ?>
    <article class="log-item<?= $blocked ? ' blocked' : '' ?>">
      <div class="log-top">
        <span class="log-host"><?= h($name !== '' ? $name : '—') ?></span>
        <span class="log-type"><?= h($qtype) ?></span>
      </div>
      <?php if ($ans !== ''): ?>
        <div class="log-ans">→ <?= h($ans) ?></div>
      <?php endif; ?>
      <div class="log-foot">
        <span class="log-time"><?= $ts !== false ? h(date('m-d H:i:s', $ts)) : '—' ?></span>
        <?php if ($client !== ''): ?><span class="log-sep">·</span><span><?= h($client) ?></span><?php endif; ?>
        <?php if ($elapsed !== ''): ?><span class="log-sep">·</span><span><?= h($elapsed) ?> ms</span><?php endif; ?>
        <?php if ($cached_hit): ?><span class="tag tag-soft">缓存</span><?php endif; ?>
        <?php if ($up !== ''): ?><span class="log-sep">·</span><span class="log-up"><?= h($up) ?></span><?php endif; ?>
      </div>
    </article>
  <?php endforeach; ?>
</section>
<?php endif; ?>

<?php endif; ?>
