# 独立 SaaS Web 前端

这里是指纹多账号管理器的独立 Web 控制台。它只负责 SaaS 业务界面和服务端 API 调用，不把登录、工作区、成员和账号目录逻辑嵌回 Chromium WebUI。

## 本地运行

将 `saas-web/` 作为静态目录交给 `saas-server`：

```powershell
$env:SAAS_WEB_DIR = "F:\mywork\chrome-finger\saas-web"
fingerprint-saas
```

服务默认监听 `127.0.0.1:8787`，打开 `http://127.0.0.1:8787/` 即可。服务端仍然需要 PostgreSQL、`SAAS_DATABASE_URL` 和 `SAAS_JWT_SECRET`；前端没有内置账号、演示数据或模拟接口。

如果前端和 API 部署在不同来源，可在 `index.html` 的 `saas-api-base` 元数据中填写 API 根地址，并在服务端配置 `SAAS_ALLOWED_ORIGINS`。生产部署应使用 HTTPS 反向代理。

## 原生桥接契约

受支持的 Chromium 客户端可以在已编译白名单来源的页面上下文中注入 `window.saasBridge`。页面通过 `bridge-contract.js` 暴露的 `window.saasBridgeClient` 访问它，当前契约主版本为 `1`，能力分为：

- `tabs`：创建、列出、激活、导航和关闭隔离 Tab；
- `storage`：读取和写入 Cookie、LocalStorage、SessionStorage 等快照；
- `fingerprint`：读取和配置账号级指纹；
- `files`：读写系统权限允许的绝对路径；相对路径以浏览器配置目录下的 `SaasFiles` 为根；
- `http`：自定义方法、请求头、Base64 正文和可选会话凭据；默认不携带浏览器 Cookie。
- `crypto`：为没有 WebCrypto 的 HTTP 页面提供原生快照加解密，使用与 HTTPS 页面兼容的 PBKDF2-HMAC-SHA-256 和 AES-256-GCM 信封。
- `secureStorage`：可选的来源隔离安全文本存储；安卓使用 Keystore，不返回密钥，未实现的平台不宣称可用。

普通浏览器没有 `window.saasBridge` 时，前端会显示“原生桥未连接”，但不会用假数据替代真实能力。桌面文件接口支持绝对路径，单文件上限 64 MiB；HTTP 响应上限 16 MiB、超时 30 秒。服务端业务页面仍可正常管理真实工作区和账号目录，Android 适配仍按同一契约推进。

## 安全约束

`session-persistence.js` 把会话业务留在独立 Web：有安全能力时只保存刷新令牌、设备标识和真实刷新接口地址，访问令牌/用户信息/快照密码不持久保存，恢复仍需服务端刷新验证。原生保护模式和退出标记不含秘密；端口尚未重连或原生写入失败不降级明文，旧写入不能覆盖退出状态。新客户端首次发现安全能力后迁移旧 SessionStorage 会话并清除缓存；普通浏览器/桌面过渡端没有该能力时保留原有 SessionStorage 行为。此方案保护设备上的密钥/密文，不防止受信 SaaS 页面被 XSS 控制；生产页面仍要求 HTTPS。

局部会话测试：`node --test saas-web/session-persistence.test.cjs saas-web/session-app.test.cjs`，17 项通过，覆盖写入/退出竞争、旧读取、服务地址隔离、能力失联、保护页面重载、缓存清理失败、设备身份一致性、用户切换、断网/401 与实际登录表单快照；测试桥/网络/界面探针不替代真实业务或 Android 平台验收。按本轮用户要求仅编码与局部验证，整体验收暂缓。

账号目录中的“同步”“恢复”以及批量按钮直接调用服务端快照、版本与租约接口。快照密码只保留在当前页面内存中，刷新、退出或锁定后需重新输入；旧 WebUI 快照使用原来的登录密码解锁。同步内容可逐项选择，网页存储始终携带来源页面，未选择类别不会在恢复时覆盖本地数据。版本冲突提供保留云端、合并和明确覆盖，提交采用刚读取的具体版本；相同 Cookie 身份和网页存储键合并时以本地为准，不同来源的网页存储不能合并。

“运行中 Tab”显示当前工作区账号的真实本机环境，可切换、关闭和编辑指纹；“设备与安全”读取并撤销真实设备会话。普通浏览器未连接原生桥时仍可管理账号目录和会话，但不能伪造本机 Tab 或存储结果。

“成员与权限”通过真实接口邀请成员、调整角色和移除成员；登录页支持接受邀请。账号目录提供授权和审计入口，授权结果由服务端逐次校验，账号查看者不能写入云端快照，账号授权不会提升工作区查看者的权限。

局部验证：`node --test saas-web/snapshot-sync.test.cjs`，共 9 项。测试使用真实 WebCrypto 验证加密往返、账号绑定、错误密码、旧信封兼容、合并冲突和锁定/会话切换停止写入；HTTP 回退与版本竞争只在测试隔离上下文中验证接口边界。

页面状态契约：`node --test saas-web/ui-state.test.cjs`，共 3 项，检查真实加载/详情/运行状态、批量租约分类、进度条无障碍属性、成员授权错误入口和设备安全摘要挂点，不注入账号、成员、设备或原生桥假数据。

桌面 iframe 仅与 `chrome://fingerprint-manager` 宿主通信；`node --test saas-web/bridge-contract.test.cjs` 验证固定目标来源、拒绝伪造父来源和失败清理。Android 客户端新增原生消息端口传输，初始化为 JSON 字符串，收到撤销或页面离开时拒绝未完成请求；真实端口／网页／JVM 互通测试位于 `android-bridge/tests/port-interop.cjs`，不代表 APK 运行验收。

- 原生桥必须由 Chromium 客户端按完整 Origin（协议、主机、端口）校验后注入；`SAAS_ALLOWED_ORIGINS` 只控制 API 的跨源访问，不能代替客户端桥接授权。
- 独立静态部署应设置响应头 `Content-Security-Policy: frame-ancestors 'self' chrome://fingerprint-manager`，不允许第三方网页把控制台嵌入并冒充原生宿主；由 `saas-server` 托管时已提供该头。
- 文件和网络能力使用类型明确的接口；文件能力仅授予编译白名单来源，不向普通页面暴露任意进程执行或裸 IPC。
- 生产环境中获得原生能力的 SaaS 页面使用 HTTPS；HTTP 仅用于本机开发或受控内网。
- Cookie、网页存储、代理凭证和指纹配置不写入前端日志，也不写入 URL。
