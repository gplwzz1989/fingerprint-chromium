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
- `SAAS_ACCESS_TOKEN_TTL`：访问令牌有效期，默认 `15m`
- `SAAS_REFRESH_TOKEN_TTL`：刷新令牌有效期，默认 `720h`
- `SAAS_ALLOWED_ORIGINS`：允许浏览器 WebUI 访问的来源，多个来源用逗号分隔；不设置时不允许跨源请求

服务启动时会执行幂等数据库迁移。迁移不会创建用户、工作区或账号，初始数据必须通过受控的管理流程写入数据库。

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
- `GET /api/v1/workspaces`
- `GET /api/v1/workspaces/{workspace_id}/accounts`
- `POST /api/v1/workspaces/{workspace_id}/accounts`
- `GET /api/v1/accounts/{account_id}/snapshot`
- `PUT /api/v1/accounts/{account_id}/snapshot`
- `POST /api/v1/accounts/{account_id}/leases`
- `DELETE /api/v1/accounts/{account_id}/leases/{lease_id}`
- `GET /api/v1/accounts/{account_id}/audit-events`
- `GET /healthz`

快照接口只接受客户端加密信封，并使用 `If-Match` 做乐观并发控制。

## 安全边界

- 密码只接收 HTTPS 请求，并使用 Argon2id 哈希后存储。
- 访问令牌为短期 JWT，刷新令牌只保存 SHA-256 摘要。
- 错误响应不返回数据库、密码哈希或令牌原始错误。
- Cookie、LocalStorage、代理凭证和指纹配置不得写入服务日志。
- 生产部署必须在 TLS 终止和访问控制完善的反向代理之后运行。
