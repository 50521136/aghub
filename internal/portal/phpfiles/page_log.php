<?php
/**
 * 查询日志。数据来自 AGHub，只显示属于当前账号的那些请求。
 */

declare(strict_types=1);

/** log_time 把日志里的时间戳变成人话。 */
function log_time($v): string
{
    if (!is_string($v) || $v === '') {
        return '—';
    }

    $ts = strtotime($v);

    return $ts === false ? $v : date('m-d H:i:s', $ts);
}

/** log_answer 把应答压成一行。 */
function log_answer($answer): string
{
    if (!is_array($answer) || $answer === array()) {
        return '—';
    }

    $parts = array();
    foreach (array_slice($answer, 0, 2) as $a) {
        if (!is_array($a)) {
            continue;
        }

        $v = isset($a['value']) ? (string) $a['value'] : '';
        $t = isset($a['type']) ? (string) $a['type'] : '';
        if ($v !== '') {
            $parts[] = $t !== '' ? $t . ' ' . $v : $v;
        }
    }

    $more = count($answer) - 2;

    return $parts === array() ? '—' : implode('，', $parts) . ($more > 0 ? ' 等 ' . count($answer) . ' 条' : '');
}
?>
<section class="hello">
  <div>
    <h1>查询日志</h1>
    <p class="sub">只显示你自己的请求，最近 <?= count($log) ?> 条。</p>
  </div>
  <div class="row">
    <a class="btn<?= (isset($_GET['limit']) && (int) $_GET['limit'] === 100) ? ' on' : '' ?>" href="<?= h(page_url('log', array('limit' => 50))) ?>">50 条</a>
    <a class="btn<?= (isset($_GET['limit']) && (int) $_GET['limit'] === 100) ? ' on' : '' ?>" href="<?= h(page_url('log', array('limit' => 100))) ?>">100 条</a>
    <a class="btn" href="<?= h(page_url('log')) ?>">刷新</a>
  </div>
</section>

<section class="card">
<?php if ($log === array()): ?>
  <p class="hint">还没有记录。客户端开始用上面的地址解析之后，这里就会出现。</p>
<?php else: ?>
  <div class="tablewrap">
  <table class="log">
    <thead>
      <tr>
        <th>时间</th>
        <th>域名</th>
        <th>类型</th>
        <th>结果</th>
        <th>来源</th>
        <th class="num">耗时</th>
      </tr>
    </thead>
    <tbody>
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
        $cached = !empty($e['cached']);
      ?>
      <tr>
        <td class="nowrap"><?= h(log_time($e['time'] ?? '')) ?></td>
        <td class="host"><?= h($name !== '' ? $name : '—') ?></td>
        <td><span class="tag"><?= h($qtype) ?></span></td>
        <td class="ans"><?= h(log_answer($e['answer'] ?? array())) ?></td>
        <td class="nowrap"><?= h($client !== '' ? $client : '—') ?></td>
        <td class="num nowrap">
          <?= h($elapsed !== '' ? $elapsed . ' ms' : '—') ?>
          <?= $cached ? '<span class="tag soft">缓存</span>' : '' ?>
        </td>
      </tr>
    <?php endforeach; ?>
    </tbody>
  </table>
  </div>
<?php endif; ?>
</section>
