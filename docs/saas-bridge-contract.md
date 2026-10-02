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
| `tabs` | 已接入 | 待接入 | 单父窗口内的原生 Tab |
| `storage` | 已接入 | 待接入 | Cookie、LocalStorage、SessionStorage 快照 |
| `fingerprint` | 已接入 | 待接入 | 与账号 Tab 绑定的指纹配置 |
| `files` | 已接入绝对路径 | SAF 授权 URI | 桌面端支持系统绝对路径；相对路径仍以 `SaasFiles` 为根，不能引用上级目录 |
| `http` | 已接入首版 | 原生网络适配 | 支持自定义方法、请求头、Base64 正文和可选会话凭据；单次响应上限 16 MiB、超时 30 秒 |
| `crypto` | 已编码，待运行联调 | 待接入 | HTTP 页面的原生 PBKDF2/AES-GCM 加密，与 HTTPS WebCrypto 信封兼容 |

文件能力沿用原生桥完整 Origin 白名单，可以读写当前进程系统权限允许的绝对路径，相对路径保留专用目录兼容行为。读写在后台线程执行，单文件上限 64 MiB；写入采用同目录临时文件与替换流程。原生文件辅助函数已通过真实 Windows 文件系统测试，包括中文路径、二进制、空文件和失败保留原内容；客户端桥接运行联调仍待完成。原生 HTTP 默认不携带浏览器 Cookie，调用方明确设置 `includeCredentials` 后才携带。未接入的平台继续报告能力不可用，不使用模拟数据替代。

## Android 适配边界

原生 `crypto.encryptSnapshot` 接收 `{accountId, password, snapshot}`，返回标准加密信封；`crypto.decryptSnapshot` 接收 `{accountId, password, envelope}`，返回验证账号和版本后的快照。两个方法沿用同一来源白名单，在后台线程派生密钥，不保存密码或快照明文，不输出原始加密库错误。未实现该能力的平台必须报告 `crypto: false`。

Android 原生层只实现 `SaasBridge` 契约，不复制 SaaS 业务逻辑。Tab 生命周期与 Activity/任务栈分离，账号数据目录由应用私有存储管理；会话密钥使用 Android Keystore，应用进程被回收后通过账号标识恢复，而不是依赖进程常驻。
