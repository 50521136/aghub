<?php
/**
 * AGHub 门户 —— 配置。
 *
 *     cp config.sample.php config.php
 *
 * 改完 AGHUB_URL 和 AGHUB_TOKEN 就能跑，没有别的必填项。
 */

return array(

    // AGHub 的地址，带端口。这一跳是 PHP 在服务端访问的，服务端到服务端，
    // 不受浏览器策略约束 —— 所以 AGHub 是 http 还是 https、有没有证书，
    // 跟你的站点是不是 https，都没有关系。不要带结尾斜杠。
    //
    //   国内 AGHub 跑明文：  'http://36.133.104.222:3000'
    //   AGHub 上了证书：     'https://api.adguardhome.lv10.ren'
    'aghub_url' => 'http://127.0.0.1:3000',

    // 对接令牌。AGHub 管理端 → 门户 → 「对接令牌」复制过来。
    // 部署包里已经带了一份，解压出来直接就有，不用手动填。
    'token' => '',

    // 连 AGHub 时是否校验 TLS 证书。AGHub 用自签证书时改成 false，
    // 否则 PHP 会因为证书不受信任而连不上（页面会显示 502）。
    'verify_tls' => true,

    // 连 AGHub 的超时（秒）。
    'timeout' => 10,

    // 站点标题，显示在页头和浏览器标签上。
    'title' => 'DNS 服务',
);
