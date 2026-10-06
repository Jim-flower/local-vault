# Vault 使用指南

Vault 是一个供个人使用的本地密码管理器，支持桌面应用和浏览器访问，可以管理密码条目、分类、TOTP 验证码及编辑历史，并通过加密 ZIP 导入、导出数据。浏览器版仅使用固定的 `admin` 账号，支持强制登录 2FA；项目管理和成员管理功能已移除。

浏览器部署中，应用提供 HTTP 服务，网关负责 HTTPS。公开路径前缀在启动时配置，同一个镜像可以部署在 `/`、`/vault/` 或 `/tools/vault/`，修改前缀无需重新编译前端。

## 1. 选择使用方式

| 方式 | 启动后如何访问 | 适用场景 |
| --- | --- | --- |
| 桌面应用 | 打开 `vault.exe` | 在自己的电脑上直接管理密码 |
| 本机浏览器 | `http://localhost:8787/` | 不使用桌面窗口，通过浏览器操作 |
| Docker + HTTPS 网关 | 本机初始化后，通过公开 HTTPS 地址访问 | 在服务器部署或从其他设备访问 |

桌面版直接使用主密码解锁。浏览器版先通过账号密码和配置要求的 2FA 验证，再用主密码解锁密码库。登录 2FA 不改变密码库的加密方式，也不影响桌面版的主密码解锁流程。

## 2. Docker 快速开始

先安装并启动 Docker，确保 `docker compose` 可用，然后在项目目录执行：

```bash
cp .env.example .env
docker compose build app
docker compose run --rm --no-deps app -generate-login-2fa-secret
```

Windows PowerShell 中，复制配置文件也可以用：

```powershell
Copy-Item .env.example .env
docker compose build app
docker compose run --rm --no-deps app -generate-login-2fa-secret
```

已有 `.env` 时直接修改它，不要用示例文件覆盖现有配置。

生成命令仅输出一个新的 Base32 密钥，不启动服务，也不会打开数据库。将这个密钥手动添加到手机验证器，选择 TOTP、SHA-1、6 位、30 秒周期；然后在 `.env` 中填写同一个密钥：

```dotenv
DEVHUB_REQUIRE_LOGIN_2FA=1
DEVHUB_ADMIN_TOTP_SECRET=这里替换成刚生成的Base32密钥
```

配置的是长期密钥，不是手机上不断变化的六位验证码。保存配置后启动：

```bash
docker compose up -d
```

Compose 默认要求登录 2FA。密钥缺失、格式不正确或不足 20 字节时，服务拒绝启动；需要明确关闭时设置 `DEVHUB_REQUIRE_LOGIN_2FA=0`。

启动后打开 **`http://localhost:8787/`**，创建初始管理员：

1. 管理员用户名固定为 `admin`，没有默认密码。
2. 设置至少 12 个字符的账号密码。
3. 为新密码库设置至少 12 个字符的主密码。
4. 填写手机验证器中的当前六位验证码，完成初始化后进入密码库。

如果使用的是已有密码库，填写原来的主密码，不要尝试创建另一个主密码。桌面版创建的密码库在首次使用浏览器版时，也需要完成管理员账号初始化。

默认端口分别承担以下用途：

| 地址 | 用途 | 路径 |
| --- | --- | --- |
| `http://localhost:8787/` | 本地使用、首次管理员初始化 | 始终为 `/` |
| `http://127.0.0.1:8788/` | HTTPS 网关的 HTTP 上游 | 由 `DEVHUB_BASE_PATH` 决定 |

Compose 将两个端口都绑定在宿主机的 `127.0.0.1`。公开入口拒绝创建初始管理员；默认配置下，对公开入口的请求还需要网关提供 HTTPS 转发标记，因此不要直接把 `http://localhost:8788/` 当作本地初始化地址。

### 在远程服务器上初始化

从自己的电脑建立 SSH 隧道：

```bash
ssh -N -L 18787:127.0.0.1:8787 user@server
```

将 `user@server` 替换为服务器的 SSH 登录信息。保持隧道运行，在本机打开 `http://localhost:18787/` 完成初始化，结束后关闭 SSH 隧道。

## 3. 账号密码、主密码和 ZIP 密码

| 密码 | 用途 |
| --- | --- |
| 账号密码 | 登录浏览器版固定的 `admin` 账号 |
| 密码库主密码 | 解锁密码库、确认删除条目 |
| 导出 ZIP 密码 | 加密导出文件；导入该文件时使用 |

这些密码可以分别设置。登录 2FA 的长期密钥另外保存在部署配置和手机验证器中，不要只保存在尚未解锁的 Vault 自己里面。

浏览器版只接受 `admin` 登录，没有成员管理入口。升级后，原来的 `admin` 密码和密码库数据继续使用；旧成员记录不会自动删除，但不能再登录。

### 登录 2FA 的配置与恢复

启用后，本地维护入口和公开入口的登录都需要六位验证码，首次管理员初始化也需要验证。只有账号密码与验证码同时正确才会发放登录会话；随后仍需密码库主密码解锁。

验证码允许前后各一个 30 秒周期的时钟偏差；成功使用后不能再次使用，服务重启也不会重置这个防重放记录。退出后立刻重新登录时，如当前验证码已用过，等待下一组验证码。连续失败会触发登录限流。

当前方案由部署环境持续管理密钥，没有账号绑定页面或一次性恢复码。手机丢失时，部署管理员可以生成新的密钥，重新配置手机验证器与 `DEVHUB_ADMIN_TOTP_SECRET`，然后执行 `docker compose up -d`。修改密钥不改变账号密码和密码库主密码，但重建服务会使旧会话失效。

原生 Web 进程可使用环境变量：

```bash
export DEVHUB_REQUIRE_LOGIN_2FA=1
export DEVHUB_ADMIN_TOTP_SECRET='这里替换成你的Base32密钥'
```

Windows PowerShell 对应：

```powershell
$env:DEVHUB_REQUIRE_LOGIN_2FA = '1'
$env:DEVHUB_ADMIN_TOTP_SECRET = '这里替换成你的Base32密钥'
```

随后在同一终端启动 Web 服务。原生程序不会读取 `.env`，未指定启用标记时默认不要求登录 2FA。密钥不会返回给浏览器，也不会写入数据库；数据库只保存防重放状态。不要将包含密钥的 `.env` 提交到 Git。

## 4. 日常使用

### 分类和条目

1. 点击 **New Category** 新建分类，或选择现有分类。
2. 点击 **Add Entry** 添加条目，名称和密码必填；可选填用户名、网址、备注及 2FA 密钥。
3. 点击密码生成按钮，可生成 20 位随机密码并填入表单。
4. 点击条目查看详情，使用复制按钮复制密码、用户名或网址。
5. 点击 **Edit** 修改条目；名称需要在整个密码库中保持唯一。
6. 使用搜索框查找条目，或点击分类筛选。

删除分类会将其中的条目移到 `General`，不会删除这些条目。`General` 分类不能删除。

### TOTP 验证码和历史记录

在条目的 2FA 密钥字段填写服务提供的 **Base32 密钥字符串**，不要填二维码图片或完整的 `otpauth://` 地址。当前实现生成 SHA-1、6 位、30 秒周期的 TOTP 验证码，使用服务端时间；验证码不匹配时先检查服务器时钟。

打开条目详情，点击 **History** 查看编辑前保存的历史版本。新条目在首次编辑之前没有历史记录。当前界面提供历史查看，没有一键恢复按钮；需要恢复时，将历史内容手动填回编辑表单。

### 删除、锁定和退出

- 删除单条或批量条目时，界面要求重新输入主密码并完成抓娃娃验证。删除操作没有回收站。
- 点击侧栏顶部的 **Lock vault** 锁定密码库。在浏览器版中，该操作会锁定当前服务实例的密码库，并清除所有浏览器会话的解锁状态，你在其他浏览器中的会话也需要重新解锁。
- 点击 **Sign out** 退出自己的浏览器账号。退出账号与锁定整个密码库是两个不同操作。
- 浏览器账号会话最长有效 12 小时，超过 30 分钟没有通过会话校验的请求后失效；服务重启也会清除会话。账号会话过期不等于服务端自动清除密码库密钥，离开时请主动锁定。

## 5. 导出、导入与备份

### 用加密 ZIP 迁移条目

解锁后点击 **Export**，设置并确认至少 8 个字符的 ZIP 密码：

- 桌面版选择保存位置。
- 浏览器版通过浏览器下载文件。
- 导出包含全部分类和当前条目，包括条目的 TOTP 密钥，不受当前筛选条件影响。

在目标密码库完成初始化并解锁后，点击 **Import**，填写 ZIP 密码，再选择导出文件。导入使用目标密码库的主密码重新加密数据，因此目标库可以使用不同的主密码。

**同名条目会被跳过，不会覆盖。** 如需替换某个同名条目，先确认需要保留哪份内容，再手动修改或处理冲突。界面会报告导入及跳过的条目数量。

ZIP 导出不包含浏览器账号、编辑历史或原密码库的主密码材料，不能替代完整数据库备份。浏览器上传请求最大为 16 MiB；导入 ZIP 中解压后的 JSON 最大为 10 MiB。

### 完整数据库备份

桌面版和本机浏览器版默认共用当前操作系统用户目录中的 `.vault.db`：

- Windows：`%USERPROFILE%\.vault.db`
- macOS / Linux：`~/.vault.db`
- Docker：`devhub-data` 数据卷中的 `/data/.vault.db`

复制数据库之前，关闭使用该库的桌面应用和浏览器服务。数据库备份保留账号、条目和编辑历史，恢复后仍需要原密码库主密码。

数据库备份不包含登录 2FA 密钥。恢复部署时还需要原来的部署配置，或重新生成密钥并配置手机验证器。

Docker 中可以停止服务后复制整个数据目录，将备份放在代码仓库之外：

```bash
docker compose stop app
docker cp devhub:/data/. ../vault-data-backup
docker compose start app
```

`vault-data-backup` 应使用新的备份目录，避免混入旧备份。保管好数据库备份和 ZIP 密码；忘记主密码后，没有内置的密码找回流程。

恢复完整数据库时：

1. 停止应用，并备份目标部署当前的数据。
2. 将备份数据还原到同一用户的 `.vault.db` 位置，或 Docker 的 `devhub-data` 数据卷中。存在 SQLite 日志文件时，将它们与对应数据库一起处理，不要混用不同备份的文件。
3. Docker 中确认还原文件允许容器用户 `10001:10001` 读写。
4. 启动应用，用备份中的账号密码和原主密码登录、解锁，并检查条目。

名称、用户名和网址以明文保存在数据库中；条目密码、备注和 TOTP 密钥经过加密。数据库文件和完整备份仍应作为敏感文件保管。

## 6. 配置公开路径前缀

在 `.env` 中配置公开入口的路径：

```dotenv
DEVHUB_BASE_PATH=/vault/
```

然后执行：

```bash
docker compose up -d
```

此时公开入口的页面、资源和 API 分别位于 `/vault/`、`/vault/assets/...` 和 `/vault/api/...`。`/vault` 会跳转到 `/vault/`。本地初始化入口仍是 `http://localhost:8787/`。

**网关需要匹配并保留 `/vault`，不能再 strip。** 更换为 `/tools/vault/` 时，同步更新网关的匹配路径；恢复根路径时设置 `DEVHUB_BASE_PATH=/`。

前端构建使用相对资源路径，Go 返回入口 HTML 时根据环境变量提供 `<base href="...">`，API 和 Cookie 路径也使用同一配置。因此只需重新创建容器加载配置，不需要重新编译前端或镜像。更换前缀或重启服务后，浏览器需要重新登录。

## 7. 网关 TLS 配置

请求经过以下链路：

```text
浏览器 HTTPS → 网关终止 TLS → Vault HTTP 上游 :8788
```

默认保持 `DEVHUB_REQUIRE_HTTPS=1`。它检查浏览器原始请求是否为 HTTPS，不要求网关到应用的上游连接也使用 TLS。网关需要保留公开 Host，并覆盖 `X-Forwarded-Proto` 和 `X-Forwarded-For`。上游只能由可信网关访问。

### 独立域名，根路径

设置 `DEVHUB_BASE_PATH=/`。同一宿主机上的 Caddy 配置示例：

```caddy
vault.example.com {
    reverse_proxy 127.0.0.1:8788
}
```

将域名替换为自己的域名，并配置 DNS 和网关所需的 TLS 条件。Caddy 会提供转发请求头。

### 共用域名，子路径

设置 `DEVHUB_BASE_PATH=/vault/`。在已有 Nginx HTTPS `server` 块中添加：

```nginx
location = /vault {
    return 308 /vault/$is_args$args;
}

location /vault/ {
    # 不加末尾斜杠：上游收到的路径仍是 /vault/...
    proxy_pass http://127.0.0.1:8788;
    proxy_set_header Host $http_host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $remote_addr;
    client_max_body_size 16m;
}
```

这是已有 HTTPS 网关中的路由片段，不包含证书和完整的 `server` 配置。若设置了不同的 `DEVHUB_REMOTE_VAULT_PORT`，同步修改上游端口。

网关在另一个容器中运行时，`127.0.0.1` 指向网关容器自身，需要将两个容器加入同一私有 Docker 网络，再使用 `app:8788` 等可解析的服务地址连接。

如果 TLS 在隧道服务商处终止，内层网关收到的可能已经是 HTTP；这时需要从可信链路传递原始 HTTPS 信息，而不能直接把内层 `$scheme` 当作浏览器协议。相关示例见 [DOCKER.md](DOCKER.md)。

本机 `8787` 可以直接通过 HTTP 使用。受控私有网络中需要让公开监听器接受 HTTP 时，可以显式设置 `DEVHUB_REQUIRE_HTTPS=0` 并执行 `docker compose up -d`，但 Compose 的宿主机端口仍绑定在回环地址，且该浏览器连接不加密。不要用它替代公开 HTTPS 部署。

## 8. 配置、更新和运维

| `.env` 配置 | 默认值 | 含义 |
| --- | --- | --- |
| `DEVHUB_PORT` | `8787` | 宿主机本地维护端口 |
| `DEVHUB_REMOTE_VAULT_PORT` | `8788` | 宿主机公开入口的上游端口 |
| `DEVHUB_BASE_PATH` | `/` | 公开入口运行时路径前缀 |
| `DEVHUB_REQUIRE_HTTPS` | `1` | 公开入口要求原始浏览器请求使用 HTTPS |
| `DEVHUB_REQUIRE_LOGIN_2FA` | Compose 为 `1`；原生为 `0` | 浏览器登录和首次初始化要求 TOTP 验证；仅接受 `0` / `1` |
| `DEVHUB_ADMIN_TOTP_SECRET` | 空，启用 2FA 时必填 | 与手机验证器一致的 Base32 密钥，至少编码 20 字节 |
| `TZ` | `Asia/Shanghai` | 容器时区；不代替系统时钟同步 |

这些端口配置调整的是宿主机映射，容器内部仍使用 `8787` 和 `8788`。

```bash
# 查看状态和日志
docker compose ps
docker compose logs -f app

# 修改 .env 后应用配置，不构建镜像
docker compose up -d

# 拉取代码并重新构建，适用于源码更新
git pull --ff-only
docker compose up -d --build

# 停止并移除容器，保留密码库数据卷
docker compose down
```

不要为了更新代码删除 `devhub-data`。`docker compose down -v` 会删除该数据卷。

旧版本升级到运行时前缀版本时，需要先构建一次，并取消网关中的旧 strip 规则。项目管理代码已移除，旧 `.devhub.db` 不再打开，但不会被自动删除。Compose 项目名、镜像名和数据卷仍保留 `devhub` 命名，以兼容已有部署。

升级到单用户登录 2FA 版本时，先构建新版镜像并配置 2FA 密钥，再启动服务。以后更换 2FA 密钥或启用标记只需更新配置、重新创建容器，不需要重新构建镜像。

## 9. 桌面版与本机 Web 模式

已有 Windows 桌面程序时，直接打开 `vault.exe`，首次设置主密码后即可使用。源码构建需要 Go 1.25 或更高版本、Node.js/npm 和 Wails v2；Windows 构建及安装程序说明见 [PACKAGING.md](PACKAGING.md)。

```powershell
wails build
```

程序输出到 `build/bin/vault.exe`。使用同一操作系统用户运行桌面版和 Web 版时，它们读取同一个 `.vault.db`，迁移或备份之前需关闭两种进程。

Windows 本机 Web 模式可以这样启动：

```powershell
.\build\bin\vault.exe -web -no-open
```

然后访问 `http://localhost:8787/`。需要另开网关入口时：

```powershell
.\build\bin\vault.exe -web -host 127.0.0.1 -port 8787 -remote-vault-port 8788 -allow-remote -base-path /vault/ -no-open
```

原生进程的 `-base-path` 优先于 `DEVHUB_BASE_PATH` 环境变量，只影响 `-remote-vault-port` 监听器。**原生进程不会自动读取 `.env` 文件**；`.env` 由 Docker Compose 读取并传入环境变量。监听端口必须在 `1024` 到 `65535` 之间，两端口不能相同。

## 10. 常见问题

| 现象 | 检查方法 |
| --- | --- |
| 公开地址无法初始化管理员 | 先使用本地 `8787`，远程服务器可通过 SSH 隧道初始化 |
| 提示远程访问需要 HTTPS 网关 | 检查 `-allow-remote`、网关的 `X-Forwarded-Proto` 和实际 TLS 终止位置 |
| 页面空白，JS/CSS 或 API 地址不正确 | 检查 `.env` 前缀、容器是否已重新创建、网关是否还在 strip；三者需一致 |
| 改 `.env` 后没有生效 | 执行 `docker compose up -d`；仅 `restart` 不会重新加载 Compose 环境配置 |
| 提示 cross-origin request rejected | 网关需保留浏览器实际访问的 Host，包括非默认端口，并传递正确的协议 |
| 容器网关连接不到上游 | 使用同一私有网络中的服务地址，不要连接网关容器自身的 `127.0.0.1` |
| 导入成功但条目没有更新 | 同名条目默认跳过，不覆盖已有内容 |
| 登录失效或重启后需要重新解锁 | 浏览器会话和解锁状态保存在服务内存中，需要重新登录、解锁 |
| 连续登录或解锁失败后被限流 | 等待响应中的重试时间后再尝试，不要持续重试 |
| 启用 2FA 后容器反复重启 | 查看日志，确认配置的是足够长的 Base32 密钥，且密钥已传入容器 |
| 验证码提示无效或已使用 | 确认手机密钥与部署配置相同、服务端时间准确；已成功使用的验证码需要等下一周期 |
| TOTP 验证码不正确 | 检查 Base32 密钥、服务端时间和目标服务的算法/位数/周期 |

部署细节见 [DOCKER.md](DOCKER.md)，Windows 打包见 [PACKAGING.md](PACKAGING.md)，代码实现说明见 [GUIDE.md](GUIDE.md)。日常使用和当前部署行为以本指南为准。
