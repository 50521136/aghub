<?php
/**
 * AGHub 门户的 PHP 反代配置 —— 复制成 config.php 后修改。
 *
 *     cp config.sample.php config.php
 */

return array(

    // AGHub 的地址。http 或 https 都行 —— 这一跳是 PHP 去访问的，服务端到
    // 服务端，不受浏览器策略约束，所以 AGHub 那边不需要证书。
    //
    //   国内 AGHub 跑明文：  'http://36.133.104.222:3000'
    //   AGHub 上了证书：     'https://api.adguardhome.lv10.ren'
    //
    // 不要带结尾斜杠。
    'aghub_url' => 'http://36.133.104.222:3000',

    // 本程序在站点上的路径前缀，默认 portal，也就是访问 /portal/。
    // 如果你把文件放在别处，这里跟着改。
    'prefix' => 'portal',

    // 连 AGHub 的超时（秒）。
    'timeout' => 20,

    // 连 AGHub 时是否校验 TLS 证书。AGHub 用自签证书时改成 false，
    // 否则 PHP 会因为证书不受信任而连不上（浏览器里会看到 502）。
    // 用正式证书的话保持 true。
    'verify_tls' => true,
);
