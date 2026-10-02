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
- `files`：读写浏览器配置目录下的 `SaasFiles` 专用目录，路径只能是相对路径；
- `http`：自定义方法、请求头、Base64 正文和可选会话凭据；默认不携带浏览器 Cookie。

普通浏览器没有 `window.saasBridge` 时，前端会显示“原生桥未连接”，但不会用假数据替代真实能力。桌面首版文件能力限制在专用目录内，单文件上限 64 MiB；HTTP 响应上限 16 MiB、超时 30 秒。服务端业务页面仍可正常管理真实工作区和账号目录，Android 适配仍按同一契约推进。

## 安全约束

- 原生桥必须由 Chromium 客户端按完整 Origin（协议、主机、端口）校验后注入；`SAAS_ALLOWED_ORIGINS` 只控制 API 的跨源访问，不能代替客户端桥接授权。
- 文件和网络能力必须是类型明确的接口，不向网页暴露任意进程执行、裸 IPC 或未限制的文件系统根目录。
- 生产环境中获得原生能力的 SaaS 页面使用 HTTPS；HTTP 仅用于本机开发或受控内网。
- Cookie、网页存储、代理凭证和指纹配置不写入前端日志，也不写入 URL。
