# SaaS 原生能力桥接契约

## 目标

`saas-web/` 是独立部署的业务前端，Chromium 只负责提供版本化的本地能力。业务登录、工作区、账号目录、同步、权限和计费不进入 Chromium。

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
| `files` | 已接入绝对路径 | SAF 授权 URI | 桌面端支持系统绝对路径；相对路径仍以 `SaasFiles` 为根，不能引用上级目录 |
| `http` | 已接入首版 | 原生网络适配 | 支持自定义方法、请求头、Base64 正文和可选会话凭据；单次响应上限 16 MiB、超时 30 秒 |
| `crypto` | 已编码，待运行联调 | 端口宿主已编码，待 APK 验证 | HTTP 页面的原生 PBKDF2/AES-GCM 加密，与 HTTPS WebCrypto 信封兼容 |

文件能力沿用原生桥完整 Origin 白名单，可以读写当前进程系统权限允许的绝对路径，相对路径保留专用目录兼容行为。读写在后台线程执行，单文件上限 64 MiB；写入采用同目录临时文件与替换流程。原生文件辅助函数已通过真实 Windows 文件系统测试，包括中文路径、二进制、空文件和失败保留原内容；客户端桥接运行联调仍待完成。原生 HTTP 默认不携带浏览器 Cookie，调用方明确设置 `includeCredentials` 后才携带。未接入的平台继续报告能力不可用，不使用模拟数据替代。

## Android 适配边界

原生 `crypto.encryptSnapshot` 接收 `{accountId, password, snapshot}`，返回标准加密信封；`crypto.decryptSnapshot` 接收 `{accountId, password, envelope}`，返回验证账号和版本后的快照。两个方法沿用同一来源白名单，在后台线程派生密钥，不保存密码或快照明文，不输出原始加密库错误。未实现该能力的平台必须报告 `crypto: false`。

Android 原生层只实现 `SaasBridge` 契约，不复制 SaaS 业务逻辑。Tab 生命周期与 Activity/任务栈分离，账号数据目录由应用私有存储管理；会话密钥使用 Android Keystore，应用进程被回收后通过账号标识恢复，而不是依赖进程常驻。

Android 编码的首版使用 Chromium `WebContents.createMessageChannel()`，以精确目标 Origin 把端口交给真实主框架；来源读取 `RenderFrameHost.getLastCommittedOrigin()`，不信任消息自报地址。初始化和后续消息均为 JSON 字符串。网页客户端仅接受来自固定原生来源、`source=null` 的可信初始化事件；这层网页检查不代替原生来源授权。

端口请求上限 24 MiB UTF-8、2 个后台工作线程、4 个等待任务和 64 MiB 未完成请求字符内存预算；运行任务跨导航代次计费，等待任务取消或任务实际完成后才释放。JSON 总深度上限 64，响应包装超限时返回明确错误而不返回截断数据。独立路由只提供加密；绑定真实 TabModel 实现后增加 `tabs/storage/fingerprint`，`files/http` 继续为 false；尚未完成 Android 平台运行验收。

安卓存储请求使用非阻塞 UI 发起与异步 JNI 完成；原生操作上限 30 秒，宿主等待 35 秒。输入/输出 JSON 上限 14 MiB，Cookie 最多 5000 条，每类网页存储最多 10000 键、UTF-8 总量 10 MiB、键 1024 字节、值 1 MiB。同账号操作不交叉执行，Profile 内最多 8 个未释放操作；超时释放大数据，但未结束的 IPC 仍占操作额度。

`getSnapshot` 可携带 `cookies/local_storage/session_storage` 布尔选项；未选类别不读取。`writeSnapshot` 按快照 `sync_options` 处理，未选类别不清空。网页存储要求已加载的同来源 HTTP/HTTPS 文档；Cookie-only 可在空白账号环境操作。指纹快照须先通过 `fingerprint.set` 应用，写入存储时核对 UA、硬件并发数和种子，不能悄悄忽略不匹配配置。

安卓 `fingerprint.set` 支持 `user_agent` 与 `hardware_concurrency` 的部分更新，拒绝未知字段；UA 为至多 512 字节可打印 ASCII，空字符串清除覆盖，硬件值为 0～64 整数，0 表示恢复种子驱动的页面行为。种子/代理由创建环境确定，不能当作在线字段假装切换。存储操作未释放时拒绝指纹修改。

自定义 Chromium UA 的 CH 元数据仅从字符串提取，未知架构/型号留空；非 Chromium UA 使用 UA-only 覆盖，不混入本机默认 CH。更改 UA 返回 `requires_reload`，不自动重放页面 POST；下一次受控导航显式应用覆盖。配置状态的 `hardware_override_scope=page_frames`、`worker_fingerprint_verified=false` 明确说明当前不承诺全部 Worker 的一致性；配置回读不是页面/HTTP 运行验证。

网页存储按原文档弱引用与隔离世界私有标记保护，保留 `__proto__` 等真实键；配额失败尝试回滚两类网页存储，回滚失败明确报错。Cookie 使用原生规范化校验并保留 host-only/域 Cookie 与可序列化分区键；不可安全导出的分区键拒绝导出。Cookie 与网页存储不是整体原子事务，取消或失败时可能已有部分写入，不保证撤回已发出的 IPC。

标签操作由宿主切换到 UI 线程，在执行前再次检查端口、文档代次、真实主框架 Origin 和 15 秒等待期限；过期或排队超时的请求不继续修改标签。账号身份由原生固定分区或普通冻结状态读取，不能用普通标签标识冒充账号，也不能关闭控制台。创建与导航只接受 HTTP、HTTPS 和 `about:blank`，不退回默认共享分区；异常关闭自动恢复暂列低优先级。

桌面 iframe 客户端只向 `chrome://fingerprint-manager` 发送请求并接受该宿主来源的回复，不再使用通配目标来源。服务端提供 `frame-ancestors 'self' chrome://fingerprint-manager` 防止第三方嵌入；生产独立静态部署也应保留该响应头，PC Chrome 实际宿主兼容性仍需运行版验收。
