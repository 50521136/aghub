# 门户用 PHP 部署（http / https 都能用）

这个目录是给**没有反向代理、但装了 PHP** 的站点用的门户后端。它解决一个具体问题：

浏览器发起的明文 HTTP 请求会被拦掉。页面用 https 提供、接口是 http 时，浏览器按
「混合内容」直接拦下请求；跨域时还会多一道——会话 cookie 带 `SameSite=None`，明文
HTTP 下会被静默丢弃，表现是"登录提示成功、一刷新又掉线"。

这两条只约束**浏览器发出**的请求。服务端到服务端的明文 HTTP 不受影响。所以让浏览器
只跟自己同源的 PHP 说话，PHP 再去调 AGHub，AGHub 那边用 http 还是 https 都行。

```
浏览器 ──http 或 https──> 本 PHP（同源）──http 或 https──> AGHub
```

不需要给 AGHub 装证书，不需要配 CORS，不需要在管理端填「门户来源」。

## 目录结构

把 `index.php`、`config.sample.php` 和门户部署包里的 `index.html`、`config.js`
放在一起，放在站点的 `portal/` 目录下：

```
<站点目录>/portal/index.php          本目录的 index.php
<站点目录>/portal/config.php         由 config.sample.php 复制而来，只有这个要改
<站点目录>/portal/index.html         门户部署包里的
<站点目录>/portal/config.js          门户部署包里的，apiBase 保持空（同源）
```

访问 `https://你的域名/portal/` 即可。

## 配置

```bash
cp config.sample.php config.php
vi config.php
```

只需要改 `aghub_url`：

```php
'aghub_url' => 'http://36.133.104.222:3000',   // AGHub 地址，http/https 都行
```

如果 AGHub 用的是自签证书，把 `verify_tls` 改成 `false`，否则 PHP 会因为证书不受
信任而连不上，浏览器里看到 502。

## 网站服务器配置

### nginx（宝塔：网站 → 设置 → 配置文件）

```nginx
location /portal/ {
    # 交给 PHP 处理，静态文件也由 index.php 输出
    try_files $uri /portal/index.php?$query_string;
}

location ~ ^/portal/.*\.php$ {
    fastcgi_pass unix:/tmp/php-cgi-74.sock;   # 按你的 PHP 版本改
    fastcgi_index index.php;
    include fastcgi.conf;
}
```

### Apache

`.htaccess` 放在 `portal/` 目录下：

```apache
<IfModule mod_rewrite.c>
    RewriteEngine On
    RewriteCond %{REQUEST_FILENAME} !-f
    RewriteRule ^ index.php [L]
</IfModule>
```

## 常见问题

**502 / 页面显示"连不上 AGHub"**
在服务器上直接 curl 一下 `aghub_url` 看通不通：

```bash
curl -v http://36.133.104.222:3000/portal/api/public
```

不通就是网络或端口问题（AGHub 没起、防火墙没放行、地址写错）。

**登录提示成功，一刷新又掉线**
PHP 已经按浏览器实际协议兜底改写了 cookie。如果还掉线，检查站点是不是走了 CDN 或
另一层代理——那一层必须传 `X-Forwarded-Proto`，否则 PHP 判断不出浏览器是 http 还是
https。

**AGHub 用自签证书时连不上**
`config.php` 里把 `verify_tls` 设成 `false`。长期方案是给 AGHub 配正式证书，或让
`aghub_url` 走明文 HTTP（这一跳是服务端内部的，明文没有安全影响，只要 AGHub 的
端口不对外暴露）。
