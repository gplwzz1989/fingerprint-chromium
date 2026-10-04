# SaaS 原生能力桥接契约

## 目标

`saas-web/` 是独立部署的业务前端，Chromium 只负责提供版本化的本地能力。业务登录、工作区、账号目录、同步、权限和计费不进入 Chromium。

PC 与 Android 共用本契约，分别实现桌面 WebUI 宿主与 Android 主框架消息端口/Java/JNI 适配；平台入口、差异、修改范围和独立验收规则见 [项目总览第 5.4 节](../PROJECT-OVERVIEW.md#54-pc-与-android-的分工差异和联动)。契约一致不表示路径、额度、可选能力或系统机制完全相同，前端以真实能力声明为准。

## 版本与来源校验

- 当前契约主版本为 `1`，破坏性变更必须递增主版本。
- Chromium 编译参数 `fingerprint_saas_allowed_origins` 保存完整 Origin 列表，例如 `https://saas.example.com`；协议、主机和端口必须精确匹配。
- 页面通过 `postMessage` 请求能力时，Chromium 同时校验 iframe 来源、消息来源窗口和编译白名单；CORS 不能替代这层校验。
- Android 适配层必须复用相同的 Origin 校验和消息结构，不能把 Windows 路径或桌面进程标识暴露给 Web。

## 消息结构

请求：

```json
{
  "type": "fingerprint-saas-bridge:request",
  "requestId": "bridge-1",
  "method": "tabs.list",
  "args": {}
}
```

成功响应：

```json
{
  "type": "fingerprint-saas-bridge:response",
  "requestId": "bridge-1",
  "ok": true,
  "result": []
}
```

失败响应只返回用户可理解的中文错误，不返回文件系统、网络库或数据库原始错误对象。

## 能力状态

| 能力 | 桌面首版 | Android 目标 | 备注 |
| --- | --- | --- | --- |
| `tabs` | 已接入 | 已编码，待 APK 验收 | 单父窗口内的原生 Tab |
| `storage` | 已接入 | 已编码，待 APK 验收 | Cookie、LocalStorage、SessionStorage 快照 |
| `fingerprint` | 已接入 | 页面配置已编码，Worker/设备待验收 | 与账号 Tab 绑定的指纹配置 |
| `files` | 已接入绝对路径 | SAF 已编码，待设备验收 | 桌面相对路径以 `SaasFiles` 为根；安卓相对路径以该来源的用户授权目录为根 |
| `http` | 已接入首版 | Chromium 网络已编码，待设备验收 | 桌面响应上限 16 MiB；安卓 10 MiB、30 秒，自动凭据必须绑定独立账号 |
| `crypto` | 已编码，待运行联调 | 端口宿主已编码，待 APK 验证 | HTTP 页面的原生 PBKDF2/AES-GCM 加密，与 HTTPS WebCrypto 信封兼容 |
| `secureStorage` | 未接入，不宣称可用 | Keystore 已编码，整体验收暂缓 | 来源隔离的通用安全文本，不导出设备密钥、不承载 SaaS 业务 |

文件能力沿用原生桥完整 Origin 白名单，可以读写当前进程系统权限允许的绝对路径，相对路径保留专用目录兼容行为。读写在后台线程执行，单文件上限 64 MiB；写入采用同目录临时文件与替换流程。原生文件辅助函数已通过真实 Windows 文件系统测试，包括中文路径、二进制、空文件和失败保留原内容；客户端桥接运行联调仍待完成。原生 HTTP 默认不携带浏览器 Cookie，调用方明确设置 `includeCredentials` 后才携带。未接入的平台继续报告能力不可用，不使用模拟数据替代。

## Android 适配边界

`secureStorage.get({key})` 返回 `{value: string|null}`，`set({key,value})` 和 `remove({key})` 返回 `{ok:true}`；平台未实现时能力为 false。键名为 1～128 个 ASCII 字母、数字、点、下划线或短横线，值最多 16 KiB UTF-8，空文本是有效值，不等于缺失。每来源最多 16 条、应用最多 256 条。可信宿主提供真实 Origin，网页不允许自报来源或密钥；导航/超时/销毁撤销后续操作。

安卓实现使用 AndroidKeyStore AES-256-GCM 和应用私有密文，认证数据绑定版本、完整 Origin 和键名；只做本地通用存储，不识别 SaaS 登录业务。没有导出密钥、列出其他来源、清空所有来源的接口；不承诺硬件保护或防止同键旧密文回放。密文恢复但设备密钥缺失时明确失败，业务需要重新登录，不能把这种密文当作跨设备会话共享方案。

独立 Web 仅通过此能力持久保存刷新令牌、设备标识和具体刷新服务地址；恢复再由真实服务端验证，访问令牌只留内存。启用后和已保护页面重载期间不把令牌写回 SessionStorage，原生出错不降级明文；新客户端首次识别能力前的旧网页缓存会在识别后迁移并清除。退出先记录无秘密的恢复禁止标志，再清除原生条目并撤销服务端会话；原生清除失败仍不能自动恢复旧令牌。单源串行操作和版本检查防止旧请求在新登录/退出后覆盖会话。普通浏览器/桌面未接入此能力时仍使用原有 SessionStorage，生产仍要求 HTTPS，安全存储不能防止白名单站点自身的 XSS。

原生 `crypto.encryptSnapshot` 接收 `{accountId, password, snapshot}`，返回标准加密信封；`crypto.decryptSnapshot` 接收 `{accountId, password, envelope}`，返回验证账号和版本后的快照。两个方法沿用同一来源白名单，在后台线程派生密钥，不保存密码或快照明文，不输出原始加密库错误。未实现该能力的平台必须报告 `crypto: false`。

Android 原生层只实现 `SaasBridge` 契约，不复制 SaaS 业务逻辑。Tab 生命周期与 Activity/任务栈分离，账号数据目录由应用私有存储管理；会话密钥使用 Android Keystore，应用进程被回收后通过账号标识恢复，而不是依赖进程常驻。

Android 编码的首版使用 Chromium `WebContents.createMessageChannel()`，以精确目标 Origin 把端口交给真实主框架；来源读取 `RenderFrameHost.getLastCommittedOrigin()`，不信任消息自报地址。初始化和后续消息均为 JSON 字符串。网页客户端仅接受来自固定原生来源、`source=null` 的可信初始化事件；这层网页检查不代替原生来源授权。

端口请求上限 24 MiB UTF-8、2 个后台工作线程、4 个等待任务和 64 MiB 未完成请求字符内存预算；运行任务跨导航代次计费，等待任务取消或任务实际完成后才释放。JSON 总深度上限 64，响应包装超限时返回明确错误而不返回截断数据。独立路由只提供加密；绑定真实 TabModel 实现后增加 `tabs/storage/fingerprint/http`，宿主绑定 SAF 文件适配后增加 `files`；尚未完成 Android 平台运行验收。

安卓 `http.request` 的可选 `tabId` 决定账号固定分区；缺省使用独立内存网络分区、禁用 HTTP 磁盘缓存，不读取共享 Cookie。自动携带凭据必须同时提供 `includeCredentials=true` 和真实账号 `tabId`，不能同时提供显式 Cookie 头；默认可通过请求头显式提供 Cookie/Authorization。账号配置的 UA 在没有显式 User-Agent 时使用，原生 HTTP 不承诺网页 UA-CH/Worker 指纹效果。

安卓 HTTP 正文请求上限 8 MiB、响应上限 10 MiB，头最多 100 项/32 KiB；`headersList` 可选数组保留重复响应头，`headers` 重复值用换行分隔，不应直接作为请求头复用。支持有效自定义方法，CONNECT 拒绝，GET/HEAD 不允许指定正文，TRACE 不允许非空正文。传输连接、长度和代理认证头由网络层管理。仅允许无用户名密码的 HTTP/HTTPS，不绕过 TLS；同来源最多 5 次重定向，跨来源拒绝，可直接重新请求目标地址。30 秒总超时、每 Profile 4 个并发，导航/关闭/撤权停止后续操作，不能撤回已经发送的数据或服务端副作用；完整网络与凭据行为待设备验证。

安卓文件 API 使用系统 SAF 树授权，按完整 Origin 保存目录映射；`path` 是目录内相对路径，`files.list({})` 读取授权根目录。首次没有有效目录授权时打开系统选择器，不允许网页提供 URI、绝对路径或上级路径。每级条目由提供方确认父子关系，同名歧义、无法确认关系、虚拟文件直接读写均拒绝；不能绕过 Android 存储权限。

安卓文件内容为 Base64，单文件上限 16 MiB；目录最多 1000 个条目，不静默截断，未知大小为 -1。覆盖必须先独立写入并读回校验，再保留原文件备份后重命名；`files.write` 可额外返回 `backupPath`。无安全重命名能力则拒绝覆盖；不删除备份或失败临时文件，也不承诺提供方原子事务。导航、超时及持久授权撤销停止后续操作，不能撤回已经发出的提供方调用。宿主等待 120 秒，网页等待 150 秒，以容纳系统选择器；实际 Android 生命周期和权限边界仍待设备验证。

安卓存储请求使用非阻塞 UI 发起与异步 JNI 完成；原生操作上限 30 秒，宿主等待 35 秒。输入/输出 JSON 上限 14 MiB，Cookie 最多 5000 条，每类网页存储最多 10000 键、UTF-8 总量 10 MiB、键 1024 字节、值 1 MiB。同账号操作不交叉执行，Profile 内最多 8 个未释放操作；超时释放大数据，但未结束的 IPC 仍占操作额度。

`getSnapshot` 可携带 `cookies/local_storage/session_storage` 布尔选项；未选类别不读取。`writeSnapshot` 按快照 `sync_options` 处理，未选类别不清空。网页存储要求已加载的同来源 HTTP/HTTPS 文档；Cookie-only 可在空白账号环境操作。指纹快照须先通过 `fingerprint.set` 应用，写入存储时核对 UA、硬件并发数和种子，不能悄悄忽略不匹配配置。

安卓 `fingerprint.set` 支持 `user_agent` 与 `hardware_concurrency` 的部分更新，拒绝未知字段；UA 为至多 512 字节可打印 ASCII，空字符串清除覆盖，硬件值为 0～64 整数，0 表示恢复种子驱动的页面行为。种子/代理由创建环境确定，不能当作在线字段假装切换。存储操作未释放时拒绝指纹修改。

自定义 Chromium UA 的 CH 元数据仅从字符串提取，未知架构/型号留空；非 Chromium UA 使用 UA-only 覆盖，不混入本机默认 CH。更改 UA 或硬件值返回 `requires_reload`，不自动重放页面 POST；下一次受控导航显式应用覆盖。配置状态的 `worker_fingerprint_verified=false` 明确说明当前不承诺全部 Worker 的一致性；配置回读不是页面/HTTP 运行验证。

Dedicated Worker、SharedWorker 和 ServiceWorker 已使用创建时的账号有效硬件值快照，嵌套 Dedicated Worker 复制该快照；ServiceWorker 同时继承账号级 UA/UA-CH 覆盖，现有 Worker 保持创建时配置，不在修改页面配置时被静默改变。PC/Android 更改 UA 或硬件值均返回 `requires_reload`，前端提示重载而不自动重放 POST。`dedicated_worker_hardware_configured=true` 表示 Dedicated Worker 传递通道已编码，`worker_fingerprint_verified=false` 仍保留；Worker 实际网络/运行效果仍未完成，不能据此宣称整体一致。

网页存储按原文档弱引用与隔离世界私有标记保护，保留 `__proto__` 等真实键；配额失败尝试回滚两类网页存储，回滚失败明确报错。Cookie 使用原生规范化校验并保留 host-only/域 Cookie 与可序列化分区键；不可安全导出的分区键拒绝导出。Cookie 与网页存储不是整体原子事务，取消或失败时可能已有部分写入，不保证撤回已发出的 IPC。

标签操作由宿主切换到 UI 线程，在执行前再次检查端口、文档代次、真实主框架 Origin 和 15 秒等待期限；过期或排队超时的请求不继续修改标签。账号身份由原生固定分区或普通冻结状态读取，不能用普通标签标识冒充账号，也不能关闭控制台。创建与导航只接受 HTTP、HTTPS 和 `about:blank`，不退回默认共享分区；异常关闭自动恢复暂列低优先级。

桌面 iframe 客户端只向 `chrome://fingerprint-manager` 发送请求并接受该宿主来源的回复，不再使用通配目标来源。服务端提供 `frame-ancestors 'self' chrome://fingerprint-manager` 防止第三方嵌入；生产独立静态部署也应保留该响应头，PC Chrome 实际宿主兼容性仍需运行版验收。
