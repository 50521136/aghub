# AGHub

**原生 AdGuard Home + 内置用户用量管理**，一个二进制、一套系统。

AGHub 是 [AdGuard Home](https://github.com/AdguardTeam/AdGuardHome) 的 fork：DNS 服务、过滤引擎、Web UI、在线更新全部保持原样，只是**在 AGH 内部**增加了用户配额与有效期管理。不再需要「AdGuard Home + 独立 PHP 面板」两套系统，也不再需要面板通过 API 去外接 AGH。

> 部署一套 = 一个 `aghub` 二进制。用户管理页面就在 AdGuard Home 自己的控制台里。

---

## 为什么

传统做法是跑一个 AdGuard Home，再跑一个独立面板，面板通过 AGH 的 REST API 去同步客户端、统计用量、封禁超额用户。问题：

- 两套系统、两套数据库、两套认证，升级要同步两边
- 配额封禁靠定时任务轮询 API，延迟大、统计口径不一致
- 面板挂了，用户管理就瞎了

AGHub 把这一层挪进 AGH 进程内部：

- 配额判断发生在 **DNS 请求处理路径上**，不是事后轮询
- 用量统计与 AGH 的请求处理同源，不存在口径差异
- 只有一个进程、一个配置文件、一个 Web UI

## 功能

### 用户管理（内置页面 `/users`）

- 创建用户，绑定一个或多个**标识**：IP 地址、CIDR 网段、DoH/DoT 的 ClientID
- 设置**请求配额**与**统计周期**（每天 / 每月 / 总计），支持"不限"
- 设置**有效期**，到期自动拒绝；可随时延长天数
- 一键**启用 / 停用 / 重置用量 / 删除**，支持批量操作
- 概览卡片：用户总数、正常、3 天内到期、超额、已过期、已停用、本周期请求数
- 列表支持搜索、状态筛选、用量进度条、到期倒计时

### 真实拦截

配额与有效期的判断挂在 `internal/dnsforward` 的请求中间件里（`IsBlockedClient` 之后）。超配额或已过期的用户，其 DNS 请求会被直接拒绝并返回相应 reason，不经过过滤引擎。停用同理。

### 在线更新（内置页面 `/update`）

- 从 GitHub Releases 检查新版本，显示版本号、发布时间、更新说明
- 一键更新：下载 → SHA-256 校验 → 解包 → 备份旧二进制 → 原子替换 → 原地重启
- 重启用 `execve(2)` 替换进程映像，**PID 与监听端口不变**，systemd 单元不受影响，几乎零停机
- 旧版本保留为 `aghub.bak`，可手工回滚

## 构建

需要 Go 1.24+ 与 Node.js 20+。

```sh
# 前端
cd client_v2
npm ci
npm run build-prod

# 后端
cd ..
go build -o aghub .
```

交叉编译与发布打包：

```sh
VERSION=v1.0.0 REPO=owner/aghub sh ./scripts/aghub-release.sh
```

产物在 `dist-aghub/`，包含各平台归档与 `checksums.txt`。默认构建 linux/amd64、linux/arm64、linux/arm/7、linux/386、darwin/amd64、darwin/arm64、freebsd/amd64、windows/amd64，可用 `PLATFORMS` 环境变量覆盖；已有前端构建时用 `SKIP_JS=1` 跳过前端重新编译。

## 安装

```sh
mkdir -p /opt/aghub
tar -xzf aghub_1.0.0_linux_amd64.tar.gz -C /opt/aghub
chmod 0755 /opt/aghub/aghub

cp /opt/aghub/aghub.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now aghub
```

首次访问 `http://<host>:3000` 完成初始化向导，与 AdGuard Home 完全一致。

配置文件默认为工作目录下的 `AdGuardHome.yaml`，与上游格式相同，可直接沿用已有的 AdGuard Home 配置。

## 给 Web UI 和门户启用 HTTPS

跨域部署门户（前端在别的域名上）时 **API 必须是 HTTPS**：HTTPS 页面无法请求
明文 HTTP 接口（浏览器按混合内容拦掉），而且跨域登录的 cookie 带
`SameSite=None; Secure`，在明文 HTTP 下会被直接丢弃。两种情况的症状都是
"点了登录没反应"。

AGHub 自带 HTTPS，不需要反向代理：

```sh
aghub -w /opt/aghub --web-addr 0.0.0.0:3000 \
  --web-tls-cert /path/fullchain.pem \
  --web-tls-key  /path/privkey.pem
```

门户跑在独立监听器上时用另一组参数，两组互不影响：

```sh
  --portal-addr 0.0.0.0:3004 \
  --portal-tls-cert /path/fullchain.pem \
  --portal-tls-key  /path/privkey.pem
```

证书和私钥必须同时给；只给一个会拒绝启动（只给证书的监听器能接受连接但握手
永远失败，浏览器里就是一张白页）。

用 nginx 反代也可以，但必须设置 `X-Forwarded-Proto: https`，否则 AGHub 会以为
自己在明文 HTTP 上，下发的 cookie 不带 `Secure`，跨域登录同样失败。

## 在线更新的配置

更新源在编译期通过 ldflags 写入（发布脚本已自动处理）：

```
-X github.com/AdguardTeam/AdGuardHome/internal/selfupdate.Version=<版本>
-X github.com/AdguardTeam/AdGuardHome/internal/selfupdate.Repo=<owner/name>
```

运行时可用环境变量覆盖：

- `AGHUB_UPDATE_REPO` — 仓库，`owner/name` 格式
- `AGHUB_UPDATE_PROXY` — 下载加速前缀，例如 `https://gh-proxy.com/`
- `AGHUB_UPDATE_TOKEN` — 私有仓库的 GitHub token
- `AGHUB_UPDATE_API` — GitHub API 基址，默认 `https://api.github.com`，可用于 GitHub Enterprise 或自建镜像

### 发布资产命名约定

`internal/selfupdate` 按固定规则查找资产，**发布文件名必须严格匹配**，否则在线更新找不到包：

```
aghub_<version>_<os>_<arch>.tar.gz     # version 不带前导 v
checksums.txt                          # 每行 "sha256 文件名"
```

例如 tag `v1.0.0` 对应 `aghub_1.0.0_linux_amd64.tar.gz`。归档内的可执行文件应命名为 `aghub`。`checksums.txt` 缺失时会跳过校验并记录警告，但不建议省。

仓库内置 `.github/workflows/aghub-release.yaml`，打 tag 即自动构建多平台归档并发布 Release。

## HTTP API

所有接口都在 AdGuard Home 原有的 `/control` 认证之后，需要管理员登录。

用户管理：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/control/users/list` | 用户列表与汇总 |
| GET | `/control/users/export` | 导出为可再导入的 JSON |
| POST | `/control/users/add` | 新增用户 |
| POST | `/control/users/update` | 修改用户 |
| POST | `/control/users/delete` | 删除用户 |
| POST | `/control/users/reset` | 重置用量计数 |
| POST | `/control/users/toggle` | 批量启用 / 停用 |
| POST | `/control/users/import` | 导入 JSON |

在线更新：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/control/aghub/update/status` | 当前版本、仓库、更新进度 |
| GET | `/control/aghub/update/check` | 检查是否有新版本 |
| POST | `/control/aghub/update/apply` | 下载并安装最新版本 |

完整规范见 `openapi/openapi.yaml`。

## 状态文件

用户状态保存在工作目录的 `data/users.json`（`stateVersion: 1`），与 AGH 其它数据文件并列。可随时备份、迁移、导入导出。

## 数据迁移

AGHub 不提供从既有面板自动迁移的工具。如果你有现成的 AdGuard Home + 面板部署，流程是：

1. 用面板的导出功能或数据库导出客户端列表（名称 / 标识 / 请求上限 / 到期时间）
2. 整理成 AGHub 的导入 JSON（字段见 `internal/users/http.go` 的 import 请求体）
3. 在 `/users` 页面或通过 `POST /control/users/import` 导入

## 与上游的关系

AGHub 基于 AdGuard Home 源码 fork。DNS、过滤、DHCP、TLS、查询日志等全部沿用上游实现，只新增 `internal/users`、`internal/selfupdate` 两个包并在 `internal/dnsforward`、`internal/home` 做接线。上游安全更新可以通过常规的 rebase / merge 跟进。

## 许可证

GPL-3.0，与上游 AdGuard Home 一致，见 `LICENSE.txt`。
