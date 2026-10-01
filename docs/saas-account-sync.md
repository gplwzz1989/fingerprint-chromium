# 指纹账号管理器 SaaS 同步契约

本文定义浏览器本地代理与 SaaS 云端之间的账号同步边界。仓库已包含浏览器端同步客户端和 `saas-server/` 服务端第一阶段实现；本文同时作为接口、安全边界和后续扩展约束。

## 1. 系统边界

### 云端控制面

- 用户登录、租户和工作区。
- 成员角色和账号访问权限。
- 账号清单、标签、备注和分配关系。
- 账号快照版本、设备会话、租约和审计记录。
- 加密快照的保存、下载和版本冲突检测。

### 本地浏览器代理

- 维护单父窗口和原生 Tab。
- 为每个账号创建独立 `StoragePartition`。
- 在渲染器进程启动时应用账号指纹种子。
- 应用账号级代理、User-Agent 和硬件并发数。
- 读取或写入 Cookie、LocalStorage 等账号数据。
- 按账号版本向云端上传或下载快照。

云端不直接操作浏览器进程；所有浏览器动作都通过经过认证的本地代理完成。

## 2. 账号主键和快照结构

`account_id` 是跨设备稳定主键，同时用于：

- 原生 Tab 标识；
- StoragePartition 分区名；
- 指纹配置和代理配置的归属；
- 云端账号记录和审计对象。

快照分为两部分：

| 部分 | 内容 | 建议保护方式 |
| --- | --- | --- |
| 账号清单 | `tenant_id`、`account_id`、名称、标签、版本、更新时间、状态 | TLS + 服务端访问控制 |
| 账号环境 | Cookie、LocalStorage、指纹配置、代理配置、当前页面来源 | TLS + 加密后存储，优先客户端加密 |

浏览器当前已能导出和写入 Cookie、LocalStorage、代理、User-Agent、硬件并发数和指纹种子。IndexedDB、Cache Storage、Service Worker 状态不应在未实现完整一致性校验前宣称支持同步。

建议快照至少包含以下元数据：

```text
schema_version
tenant_id
account_id
revision
device_id
updated_at
storage_url
fingerprint_seed
fingerprint
proxy_rules
cookies
local_storage
```

`schema_version` 用于结构升级，`revision` 用于并发控制，不能由客户端自行递增后覆盖云端版本。

## 3. 接口契约与实现

以下路径是当前 REST 实现和版本化契约，字段语义保持不变：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/api/v1/sessions` | 用户登录并建立本地代理会话 |
| `GET` | `/api/v1/sessions` | 获取当前用户的有效设备会话 |
| `DELETE` | `/api/v1/sessions/{session_id}` | 撤销指定设备会话 |
| `POST` | `/api/v1/invitations/accept` | 通过一次性令牌加入工作区并设置新用户密码 |
| `GET` | `/api/v1/workspaces` | 获取用户可访问的工作区 |
| `POST` | `/api/v1/workspaces/{workspace_id}/invitations` | 工作区管理员生成成员邀请 |
| `GET` | `/api/v1/workspaces/{workspace_id}/accounts` | 分页获取账号清单 |
| `POST` | `/api/v1/workspaces/{workspace_id}/accounts` | 创建账号及其稳定标识 |
| `PATCH` | `/api/v1/accounts/{account_id}` | 更新账号名称和标签 |
| `GET` | `/api/v1/accounts/{account_id}/snapshot` | 获取指定版本或最新快照 |
| `PUT` | `/api/v1/accounts/{account_id}/snapshot` | 使用 `If-Match` 提交新快照 |
| `POST` | `/api/v1/accounts/{account_id}/leases` | 获取设备编辑租约 |
| `DELETE` | `/api/v1/accounts/{account_id}/leases/{lease_id}` | 释放设备编辑租约 |
| `GET` | `/api/v1/accounts/{account_id}/audit-events` | 查询账号操作审计 |

写入快照必须携带当前云端版本。版本不匹配返回冲突，客户端重新获取版本后由用户选择合并或覆盖；不能默认使用最后写入覆盖，避免不同设备丢失登录状态。

## 4. 加密和凭证边界

- 所有云端连接必须使用 TLS，并校验服务端证书。
- Cookie、LocalStorage 和代理凭证属于敏感数据，不能写入普通日志、URL、错误消息或剪贴板提示。
- 推荐采用客户端信封加密：随机生成数据密钥，用账号恢复密钥或租户密钥包装数据密钥，云端只保存密文和必要元数据。
- 访问令牌只保存在本地安全存储中，不能写入账号快照。
- 管理器为本地安装保存稳定的设备标识；设备标识不是认证凭据，重新登录同一设备会替换该设备的旧会话。
- 成员邀请令牌只保存服务端摘要，限时且只能使用一次；管理员必须通过受控渠道把原始令牌交给受邀用户，令牌不得写入日志。
- 代理密码不应放入当前 `proxy_rules` 明文字符串；后续应拆分为代理地址、认证引用和凭证密文。
- 账号导入、导出、恢复、删除和设备授权都必须写入审计事件。

## 5. 恢复流程

1. 本地代理通过用户会话获取账号清单和快照元数据。
2. 下载并解密指定账号快照。
3. 使用稳定 `account_id`、指纹种子和代理配置创建原生 Tab。
4. 等待对应 StoragePartition 和渲染器环境就绪。
5. 校验快照来源与当前页面来源一致后写入 Cookie、LocalStorage。
6. 导航到保存的页面并验证关键存储是否写入成功。
7. 上报恢复结果和失败原因，不上传 Cookie 或 LocalStorage 明文日志。

## 6. 当前实现状态和后续开发顺序

当前已具备：

- 本地代理侧的原生 Tab、独立 StoragePartition、账号级代理、指纹种子、Cookie/LocalStorage 读写和快照导入导出；
- 服务端用户会话、设备会话列表与撤销、工作区成员角色、账号目录、客户端加密快照、版本冲突、设备租约和审计记录；
- 服务端工作区成员邀请与受邀用户密码初始化；
- 浏览器 WebUI 的 SaaS 登录、工作区账号列表、加密同步和恢复；
- 浏览器 WebUI 支持单个或批量同步/恢复账号环境；批量操作按账号串行执行，单个账号失败不会中断其他账号，并返回成功数与失败原因；
- 受控的 `fingerprint-saas bootstrap-user` 首个用户初始化流程。

服务端真实数据库初始化和跨设备运行验证仍需要部署环境凭据；仓库不内置演示用户或模拟云端数据。

后续按以下顺序推进：

1. 真实 PostgreSQL 环境下的初始化、登录、同步和租约集成测试。
2. 成员移除、角色调整和 SaaS 计费/配额边界。
3. 客户端安全凭证存储和登录状态恢复策略。
4. IndexedDB 等扩展存储的明确支持范围和一致性测试。

在服务端契约和加密方案确定前，不在 Chromium 内写入伪造云端地址、测试账号或模拟同步逻辑。
