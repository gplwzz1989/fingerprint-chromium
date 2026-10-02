# 指纹账号管理器 SaaS 服务端

这是浏览器本地代理使用的真实 SaaS 控制面第一阶段实现，负责用户会话、工作区成员权限和账号目录。服务端不接触 Cookie、LocalStorage 或指纹明文；账号环境快照通过客户端加密信封保存。

## 运行要求

- Go 1.24 或更高版本
- PostgreSQL 14 或更高版本
- 反向代理提供 HTTPS

## 配置

启动前必须设置以下环境变量：

- `SAAS_DATABASE_URL`：PostgreSQL 连接字符串
- `SAAS_JWT_SECRET`：至少 32 字节的随机签名密钥

可选配置：

- `SAAS_HTTP_ADDR`：监听地址，默认 `127.0.0.1:8787`
- `SAAS_WEB_DIR`：可选的独立 Web 前端静态目录；配置后服务根路径提供 `saas-web/`，未知页面路由回退到 `index.html`
- `SAAS_ACCESS_TOKEN_TTL`：访问令牌有效期，默认 `15m`
- `SAAS_REFRESH_TOKEN_TTL`：刷新令牌有效期，默认 `720h`
- `SAAS_ALLOWED_ORIGINS`：允许浏览器 WebUI 访问的来源，多个来源用逗号分隔；不设置时不允许跨源请求
- `SAAS_RATE_LIMIT_ENABLED`：是否启用服务端限流，默认 `true`，设为 `false` 关闭
- `SAAS_RATE_LIMIT_WINDOW`：每个标识的固定计数窗口，默认 `1m`，必须大于零且不超过 `24h`
- `SAAS_RATE_LIMIT_AUTH_REQUESTS`：每个窗口内同一真实来源 IP 的鉴权请求总额度，默认 `30`
- `SAAS_RATE_LIMIT_API_REQUESTS`：每个窗口内同一已验签用户或来源 IP 的其他 API 请求额度，默认 `300`
- `SAAS_RATE_LIMIT_MAX_KEYS`：鉴权和其他 API 共用的限流条目总容量，默认 `10000`

三个限流整数参数均须在 `1` 至 `1000000` 之间；无效配置会阻止启动并返回中文错误。关闭限流时也会检查参数合法性。

服务启动时会执行幂等数据库迁移。迁移不会创建用户、工作区或账号，初始数据必须通过受控的管理流程写入数据库。

## 服务端限流

`POST /api/v1/sessions`、`POST /api/v1/sessions/refresh`、`POST /api/v1/sessions/revoke` 和 `POST /api/v1/invitations/accept` 共用基于 `RemoteAddr` 的来源 IP 配额。忽略连接端口并归一化 IPv4、IPv6，不信任 `X-Forwarded-For`、`X-Real-IP` 或 `Forwarded`；切换端点、访问令牌或转发头不能重置这份配额。

其他 `/api`、`/api/` 下的请求优先按通过现有 JWT 签名及令牌校验的用户标识计数，同一用户跨会话、跨 IP 共用配额；无令牌、无效或过期令牌按来源 IP 计数。此处只确定限流标识，业务处理仍执行原有会话及权限校验。健康检查、静态页面和 `OPTIONS` 预检跳过限流。

超限或条目容量已满时返回 HTTP `429`、`code: rate_limited`、中文消息“请求过于频繁，请稍后重试”，以及向上取整且至少为 `1` 秒的 `Retry-After`。容量满时拒绝新标识，不淘汰已有计数；已有标识的剩余额度仍可使用。计数与容量操作由互斥锁保护，使用固定长度键和到期堆，过期条目随后续 API 请求回收，拒绝请求不会延长窗口。

当前实现为单个服务处理链内的内存固定窗口限流，重启会重置计数，多实例之间不共享配额。反向代理部署时来源 IP 为实际连接服务端的代理 IP，同一代理后的鉴权请求会共用额度；当前没有可信代理转发头解析配置。

## 部署独立 Web 前端

服务端可以直接托管仓库中的独立前端：

```powershell
$env:SAAS_WEB_DIR = "F:\\mywork\\chrome-finger\\saas-web"
fingerprint-saas
```

前端不包含演示账号或模拟接口。生产部署建议由 HTTPS 反向代理提供静态资源和 API；如果前端与 API 使用不同来源，需要同时配置前端 API 根地址和 `SAAS_ALLOWED_ORIGINS`。

## 初始化首个用户

服务端不提供未受保护的公开注册接口。首次部署时，在受控的管理终端设置以下环境变量后执行一次初始化命令：

- `SAAS_BOOTSTRAP_EMAIL`：首个用户邮箱
- `SAAS_BOOTSTRAP_PASSWORD`：首个用户密码，长度 12 到 256 个字符
- `SAAS_BOOTSTRAP_WORKSPACE`：首个工作区名称
- `SAAS_BOOTSTRAP_DISPLAY_NAME`：可选的显示名称

然后执行 `fingerprint-saas bootstrap-user`。命令会先执行数据库迁移，再以事务方式创建用户、工作区和所有者成员关系；密码只保存 Argon2id 哈希，不会写入日志。初始化成功后应立即清理这些初始化环境变量。重复执行不会覆盖已有用户。

## 当前接口

已实现：

- `POST /api/v1/sessions`
- `POST /api/v1/sessions/refresh`
- `POST /api/v1/sessions/revoke`
- `GET /api/v1/workspaces`
- `POST /api/v1/workspaces`
- `POST /api/v1/workspaces/{workspace_id}/invitations`
- `GET /api/v1/workspaces/{workspace_id}/members`
- `PATCH /api/v1/workspaces/{workspace_id}/members/{user_id}`
- `DELETE /api/v1/workspaces/{workspace_id}/members/{user_id}`
- `POST /api/v1/invitations/accept`
- `GET /api/v1/workspaces/{workspace_id}/accounts`
- `POST /api/v1/workspaces/{workspace_id}/accounts`
- `PATCH /api/v1/accounts/{account_id}`
- `GET /api/v1/accounts/{account_id}/members`
- `PATCH /api/v1/accounts/{account_id}/members/{user_id}`
- `DELETE /api/v1/accounts/{account_id}/members/{user_id}`
- `GET /api/v1/accounts/{account_id}/snapshot`
- `PUT /api/v1/accounts/{account_id}/snapshot`
- `POST /api/v1/accounts/{account_id}/leases`
- `DELETE /api/v1/accounts/{account_id}/leases/{lease_id}`
- `GET /api/v1/accounts/{account_id}/audit-events`
- `GET /healthz`

快照接口只接受客户端加密信封，并使用 `If-Match` 做乐观并发控制。版本不匹配时普通版本号会返回冲突；只有客户端明确选择覆盖时才允许使用 `If-Match: *`，该操作记录为 `snapshot_overwritten` 审计事件。

独立网页的明确覆盖使用刚读取的具体版本并携带 `overwrite: true`，不会使用通配符；云端再次变化时仍返回冲突，同样记录覆盖审计。账号列表返回 `role` 有效权限，账号查看者不能写入快照，显式账号编辑权限也不能提升工作区查看者。

工作区所有者可以邀请管理员、编辑者或查看者；管理员可以邀请编辑者或查看者。创建邀请后，接口只返回一次原始邀请令牌，部署方应通过受控渠道交给受邀用户。令牌服务端只保存摘要，有效期为 7 天且只能接受一次。新用户通过 `/api/v1/invitations/accept` 设置密码；已有用户需要再次验证自己的密码后才能加入工作区。

工作区成员可以读取成员列表。所有者可以调整或移除非所有者成员；管理员只能调整或移除编辑者和查看者。成员不能修改或移除自己，所有者成员不可被移除。

账号默认对工作区成员开放。所有者或管理员可以为账号设置显式成员列表；设置后，编辑者和查看者只能访问被分配的账号，所有者和管理员仍然拥有全部访问权限。移除账号的最后一个显式成员后，账号恢复为工作区开放状态。工作区成员被移除时，其账号权限会一并清理。

## 真实 PostgreSQL 集成验证

测试需显式配置独立数据库的 `SAAS_TEST_DATABASE_URL`；未配置时跳过集成测试。测试创建随机命名的独立 schema，并在结束后清理该 schema，不使用模拟数据库。

```powershell
$env:SAAS_TEST_DATABASE_URL = 'postgres://测试用户@127.0.0.1:测试端口/测试数据库?sslmode=disable'
go test ./...
```

覆盖初始化、登录、跨设备快照读取、并发编辑租约、版本冲突、覆盖审计和会话撤销。

## 限流专项验证

在 `saas-server/` 目录执行：

```powershell
go test ./internal/config ./internal/httpapi -run RateLimit
go test ./internal/httpapi -run TestRateLimitConcurrentRequestsAndCapacity -count=20
```

限流配置、限流器和实际 HTTP 中间件另有独立测试，不依赖 PostgreSQL：覆盖超限、窗口到期精确边界、拒绝不延长窗口、中文 `429` 与 `Retry-After` 向上取整、伪造转发头、端口及 IPv4 映射地址归一化、用户跨会话和 IP 共用额度、无效/过期/无签名令牌回退 IP、健康检查/静态页面/预检豁免、容量满时保留旧计数及过期释放。并发测试使用 512 个协程，分别验证同一标识仅允许 17 次、不同标识最多分配 32 条，并连续运行 20 次通过；还验证 1000 个新标识不能淘汰已有计数。

限流任务验证中的 `go test ./...`、`go vet ./...` 和限流专项测试通过；初次专项未配置 `SAAS_TEST_DATABASE_URL`，整合后已在独立 PostgreSQL 上复测全包并通过。`go test -race` 因当前环境未提供 CGO 所需的 C 编译器而未运行；配置包的额外覆盖率采集被 Windows 拒绝执行，普通配置测试通过。这些结果不等同于生产负载或多实例限流验收。

## 服务安全边界

- HTTP 服务支持受控本机或内网部署，生产入口应使用 HTTPS；密码使用 Argon2id 哈希后存储。
- 访问令牌为短期 JWT，刷新令牌只保存 SHA-256 摘要。
- 工作区邀请令牌只保存 SHA-256 摘要，过期或接受后立即失效。
- 错误响应不返回数据库、密码哈希或令牌原始错误。
- Cookie、LocalStorage、SessionStorage、同步选项、代理凭证和指纹配置不得写入服务日志。
- 生产部署必须在 TLS 终止和访问控制完善的反向代理之后运行。
