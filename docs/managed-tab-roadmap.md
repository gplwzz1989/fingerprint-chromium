# 指纹多账号管理器开发路线

本文用于按小任务推进单父窗口原生 Tab、多账号隔离和 SaaS 账号管理功能。每个阶段先做局部验证，只有发版前整体验证才允许进行需要的批量 Chromium 编译。

## 1. 总体验收目标

- 整个应用使用一个父浏览器窗口和原生 Tab；页面内新窗口、新 Tab 导航默认复用当前 Tab。
- 每个账号 Tab 使用稳定的 `account_id` 和独立持久化 `StoragePartition`，互不共享 Cookie、网页存储、缓存、Service Worker 和网络上下文。
- 每个账号 Tab 可以独立设置指纹种子、User-Agent、硬件并发数和代理。
- 管理器页面隐藏 Chromium 内置用户系统，使用自有 SaaS 登录、工作区、成员和账号权限。
- SaaS 同步能力是必选能力；Cookie、LocalStorage、SessionStorage、指纹、代理和页面地址可以分别选择。
- 明文账号环境不写入日志、错误消息或服务端普通字段；云端只保存客户端加密快照。
- SaaS 前端和后端作为独立 Web 项目部署；Chromium 只提供受 Origin 白名单保护的 JS 原生能力桥，不承载 SaaS 业务逻辑。

## 2. 阶段状态

### 阶段 A：单父窗口和 Tab 隔离（已实现，待运行版验收）

- 普通新 Tab 和受管理页面的窗口请求进入当前父窗口。
- 页面内 URL 跳转保持当前 Tab。
- 普通新 Tab、会话恢复和历史导航使用独立的 `managedtab` 持久化分区。
- 每个分区绑定账号级代理和指纹参数。
- 画中画窗口保留 Chromium 原有行为，不强行改成普通 Tab。

运行版验收：

1. 创建两个账号 Tab，分别写入同一来源的不同 Cookie 和 LocalStorage。
2. 互相切换 Tab，确认页面只能读取本账号数据。
3. 页面执行 `window.open`、链接 `target=_blank` 和脚本新窗口，确认不产生额外父窗口。
4. 重启浏览器后确认账号标识、分区、代理和指纹参数没有串号。

### 阶段 B：本地账号环境接口（已实现）

- WebUI 可以读取和写入 Cookie、LocalStorage、SessionStorage。
- 写入时校验来源页面，导航过程中目标页面变化会使操作失败，不报告错误成功。
- 支持账号环境快照导入、导出和按账号操作。
- 管理器页面不作为账号环境参与批量同步。

暂不宣称支持：IndexedDB、Cache Storage、Service Worker 状态的云端快照读写。它们在本地分区内天然隔离，但跨设备同步前必须单独完成一致性设计和测试。

### 阶段 C：SaaS 控制面（第一阶段已实现）

- 用户登录、刷新和撤销设备会话。
- 工作区、成员角色、邀请和账号级访问权限。
- 账号目录、版本控制、租约、冲突处理和审计记录。
- 客户端加密快照，云端不接触 Cookie、网页存储和指纹明文。
- 单个和批量同步/恢复。
- 同步总能力不可关闭，未选择的类别不会覆盖本地现状。

待补充：真实 PostgreSQL 环境下的初始化、登录、同步、租约和冲突集成测试。

### 阶段 D：独立 SaaS Web 项目和原生能力桥（桌面首版已完成，Android 适配进行中）

- 前端独立为 `saas-web/`，后端继续独立为 `saas-server/`；登录、工作区、成员、账号目录、同步和权限逻辑不再新增到 Chromium WebUI。
- Chromium 只提供版本化的 `window.saasBridge` JS API；桌面首版已接入 Tab、Cookie、网页存储、指纹、受限文件和原生 HTTP，业务状态仍由独立 SaaS 服务管理。
- 通过编译时精确 Origin 白名单决定哪些 SaaS 页面可以获得桥接能力；协议、域名和端口必须完全匹配。当前白名单已接入 `build-configs/common.gn` 和管理器桥接校验。
- 页面跳转、跨域 iframe、会话失效或能力撤销后，桥接权限立即重新校验；普通网页和普通浏览器不获得这些能力。
- 现有 `chrome://fingerprint-manager/` 保留为过渡、调试和兼容入口；当前以受控 iframe 承载独立 Web，完成运行版验收后再隐藏或移除。
- 桥接层只负责本地能力和平台适配，不保存 SaaS 业务状态，不实现登录、成员、计费等业务逻辑。

### 阶段 E：SaaS HTTP 部署和生产环境（HTTP 首版已完成，生产集成待完成）

- 独立 SaaS 前端和后端已支持通过 `SAAS_WEB_DIR` 由同一 HTTP 服务托管，保留配置化监听地址、健康检查和数据库迁移；API 预检路由已覆盖。
- 生产高权限页面使用 HTTPS；HTTP 仅用于本地开发或受控内网，不能仅依赖 CORS 授予本地原生权限。
- 完成白名单构建配置、反向代理部署、真实 PostgreSQL 初始化、登录、同步、租约和冲突集成测试。
- 增加多设备并发恢复、服务端限流、审计和敏感数据不落日志测试。

### 阶段 F：Android 移动客户端（契约已开始，原生适配待工具链）

- 已建立 `android-bridge/` 契约目录；下一步建立独立 Android GN 输出和最小编译验证，不复用 Windows 的输出目录和工具链。
- 复用独立 SaaS Web 前端，通过同一套 `window.saasBridge` 契约接入 Android 原生适配层。
- 实现 Android Tab、账号隔离、指纹配置、Cookie/网页存储、Keystore 会话和应用生命周期恢复。
- 文件能力使用 Android Storage Access Framework 和授权 URI；网络能力使用 Android 原生适配，不假设存在 Windows 文件路径。
- 完成 APK/AAB 构建、安装升级、权限撤销、断网恢复和移动端账号隔离测试。

### 阶段 G：产品化和发布（未开始）

- 代理地址、认证引用和代理凭证密文分离。
- 服务端限流、配额、数据保留和计费边界。
- 账号环境删除、导入、导出和设备授权的完整审计。
- 真实多设备并发恢复测试。
- Release 配置整体编译、安装包制作、升级和回滚测试。

## 3. 编译和变更规则

- 开发验证固定使用 `build-configs/development.gn` 和独立的 `out/Development`；发版前整体测试固定使用 `build-configs/release.gn` 和 `out/Release`。
- 日常开发只做目标文件或目标测试的局部编译；先使用依赖分析和 dry-run 判断影响范围。
- 不因小功能自动重新生成 GN。
- 预计造成大范围重新编译时，必须先说明原因、影响范围和预计成本，得到明确许可后再执行。
- 阶段功能完成后，发版前再进行必要的 Release 批量编译和整体测试。
- Chromium 源码目录的实际修改必须能通过 `patches/series` 重放；服务端代码直接提交到 `saas-server/`。

## 4. 当前下一步

1. 完成两个账号 Tab 的运行版隔离验收，并记录 Cookie、LocalStorage、代理和指纹结果。
2. 用开发版目标编译验证桌面桥接，确认白名单页面能调用 Tab、存储和指纹，普通页面不能调用。
3. 完成真实 PostgreSQL 集成、HTTP 部署和生产 HTTPS 边界测试。
4. 完成文件与原生 HTTP 的权限撤销、审计、异常恢复和运行版验证；未接入的平台继续保持能力不可用。
5. 准备 Android SDK/NDK 后完成最小 Android 构建和 `android-bridge/` 平台适配。
6. 决定 IndexedDB、Cache Storage、Service Worker 是否进入 SaaS 同步范围；在决定前保持明确不支持状态。
7. 完成产品化安全项后，再申请 Windows 和 Android Release 整体编译与发布回归。
