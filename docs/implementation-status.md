# 指纹 SaaS 浏览器阶段总结

## 当前阶段

本阶段完成了独立 SaaS Web 架构、桌面原生能力桥和 Android 契约的编码工作。SaaS 业务前端与后端位于独立项目中，Chromium 只负责受 Origin 白名单保护的本地能力，不承载登录、工作区、账号目录、同步和计费业务。

## 已完成

- 开发版和 Release 双编译配置：开发版启用组件化编译，Release 关闭组件化编译；Release 保留 ThinLTO，并关闭 Widevine。
- 现有受管理 Tab 的单父窗口模型、独立持久化 StoragePartition、账号级代理、User-Agent、UA-CH、硬件并发数和指纹种子。
- Cookie、LocalStorage、SessionStorage 的读取、写入、快照导入和导出链路。
- 独立 `saas-web/` 前端：真实登录、工作区、账号目录和 `window.saasBridge` 客户端封装；普通浏览器不使用演示数据或假接口。
- 独立 `saas-server/` 后端：用户会话、工作区、成员权限、账号目录、加密快照、版本冲突、租约、审计、静态 Web 托管和 API CORS 预检。
- 服务端可配置限流：鉴权接口按真实 `RemoteAddr` IP，其他 API 按已验签用户或 IP；中文 `429`、`Retry-After`、并发安全、总容量限制和请求驱动过期回收。五项 `SAAS_RATE_LIMIT_*` 参数及部署边界见 `saas-server/README.md`，默认启用、窗口 `1m`、鉴权 `30` 次、其他 API `300` 次、总容量 `10000` 条。
- 编译时完整 Origin 白名单：协议、主机和端口精确匹配，普通来源不能调用原生桥。
- 常驻 SaaS 启动规则：PC 自动打开固定宿主页，Android 原生 Tab 初始化完成后打开控制台；关闭单页和批量关闭均保留控制台，应用退出正常释放。
- PC 和 Android 共用 `chrome/common/chrome_switches.cc` 中的默认 SaaS 地址，后续改地址只需重编译该文件并重新链接对应平台；默认来源权限随常量更新，不改 GN。
- 桌面原生桥：Tab、存储、指纹、系统绝对路径及 `SaasFiles` 相对路径文件读写和自定义 HTTP 请求。
- 独立 Web 已接入成员邀请/接受、角色管理、账号授权和审计界面；同步冲突支持保留云端、合并与明确覆盖，覆盖仍执行具体版本校验。
- HTTP 页面加密兼容：无 WebCrypto 时调用受同一白名单保护的原生 PBKDF2/AES-GCM 接口；密码只存于当前页面内存，仍需运行版联调。
- Android 平台无关桥接契约，后续使用 Android Storage Access Framework、Keystore 和原生网络适配。
- Android 独立快照加密模块：`SnapshotCrypto`、`SnapshotJson` 和 Kotlin 适配器已实现真实 PBKDF2/AES-GCM，与网页信封互通；平台接口默认不获得页面授权，具体消息宿主挂接状态见下一项。
- 本轮已补充 Android 消息端口宿主、精确来源策略、后台加密路由和 JNI 配置挂接源码；成功绑定的新宿主仅提供 `crypto`，其余能力仍为 `false`。没有生成或安装新 APK，不能把已编码挂接当作 Android 浏览器已可运行。
- 后续单代理阶段已编码安卓固定账号分区工厂、按 BrowserContext 保存的本地配置、代理/指纹种子平台钩子、JNI 创建入口、禁止共享复制与 Java 单页导航保护；`tabs` 仍未开放，冻结和重启恢复仍待接入。
- 桌面 iframe 请求固定发往原生宿主来源并校验回复 Origin；HTTP 服务增加 `frame-ancestors`，避免第三方嵌入页面伪装原生桥。
- 构建模式、同步边界、桥接消息、部署方式和测试结果文档。

## 已完成的局部验证

- `saas-server`：`go test ./...`、`go vet ./...` 通过。
- 限流专项：超限与精确到期恢复、拒绝不延长窗口、伪造转发头、用户跨会话/IP 计数、无效令牌回退 IP、豁免请求、中文 `429` 和 `Retry-After` 取整、容量保留与过期回收均通过；512 协程的同标识额度及不同标识容量测试连续运行 20 次通过。整合后已配置独立测试 PostgreSQL，再次执行 `go test ./...` 和 `go vet ./...` 通过。竞态检测因缺少 CGO 所需 C 编译器未运行，配置包额外覆盖率采集被 Windows 拒绝执行，普通配置测试通过。
- Android 独立模块：JVM 55 项、网页与 JVM 双向互通 39 项、Kotlin 适配器 8 项通过，包含错误密码、跨账号、篡改、Unicode、14 MiB 明文及 16 MiB 密文边界；只证明模块和协议兼容，未完成 Android 设备、Chromium 桥或 APK 验证。
- 本轮 Android/消息复测共 461 项通过：加密 55、来源 136、消息路由 20、JVM 网络 187、网页加密互通 39、真实端口互通 16、Kotlin 8。网络模块需 JDK11+，未接入 Android；端口初始化事件为测试上下文注入，不构成 Android 主框架或生命周期的运行验收。
- 新增 JNI 生成、Android 源文件清单语法与源码补丁反向应用检查通过；未执行 GN 重生成。网页同步 9 项、iframe 消息安全 2 项及真实 PostgreSQL 全包复测通过。
- 单代理阶段：新账号环境头文件、共享 `chrome_content_browser_client.cc` 和参数测试源码局部语法检查通过；原生参数测试对象编译及链接通过，但运行遇到入口加载错误/系统拒绝启动，未计为运行测试通过，未修改系统策略。新增 JNI 和账号环境补丁反向检查通过；网页 11 项回归通过。
- `saas-web`：Node JavaScript 语法检查通过。
- Chromium WebUI：TypeScript 静态检查通过。
- 独立 Web 加密同步：真实 PBKDF2/AES-GCM 往返、账号绑定、错误密码拒绝、同步类别过滤及旧 WebUI 信封兼容测试通过。
- 合并与冲突：Cookie 分区/域身份、网页存储同键合并、不同来源拒绝合并、覆盖遇到新版本继续拒绝等测试通过。
- 网页同步共 9 项测试通过，包含获取租约期间锁定、恢复读取 Tab 期间切换会话后停止写入。退出/切换工作区清理旧 Tab 与设备操作、密码及待处理冲突；邀请弹窗使用操作代次防止旧响应污染重新打开的表单。
- 权限：真实 PostgreSQL 验证账号只读不能被工作区编辑权限绕过、工作区只读不能被账号授权提升、邀请只能使用一次及成员移除后失权。
- 独立网页真实页面验证：登录、成员列表、邀请创建/接受、受邀成员登录、账号权限弹窗和审计记录读取通过；编辑者的邀请按钮禁用，邀请弹窗关闭后清理令牌。本次使用隔离 PostgreSQL 与 Edge 测试会话，不替代 Chromium 原生桥运行验收。
- 文件：直接复用实际原生辅助函数和现有基础库，在 Windows 实测绝对/相对路径、中文文件名、二进制、空文件和失败时保留原文件；未宣称已经完成浏览器 JS 桥接联调。
- 真实 PostgreSQL 集成：隔离测试实例下的初始化、登录、跨设备快照读取、并发租约、版本冲突、覆盖审计和会话撤销测试通过。
- 常驻控制台：六个桌面 C++ 源文件局部语法编译、TypeScript 检查、Android JNI 生成和新增补丁反向应用检查通过。
- 默认地址常量所在 `chrome_switches.cc` 已使用现有 Development 参数完成单文件对象编译，未调用 GN；还需要重链接后才能更新运行版。
- 原生桥：`management_ui_handler.cc` 使用现有开发版编译参数完成 C++ 局部语法编译。现有生成头尚无新的 Origin 白名单标记，局部检查显式补充与 `common.gn` 一致的宏定义，不修改生成文件或 GN；最终构建仍需生成正式头并链接。
- 新增 SaaS 桥接补丁可通过反向应用检查，并已加入 `patches/series`。
- 指纹配置编辑表单已在现有指纹管理器 WebUI 中实现，包含 User-Agent、硬件并发数和指纹种子输入；尚未在包含最新源码的运行版中回归。
- 未主动修改或执行 GN，未启动 Release 全量编译；局部 Ninja 目标在构建前自动尝试重生成时因工具链预检失败而停止。

## 尚未完成

- 最新源码尚未重新链接进入可展示 SaaS 页面的新开发版二进制；当前测试版仍是旧的 Development 输出。
- 独立 SaaS Web 在 Chromium 开发版中的白名单桥接、文件和 HTTP 运行联调。
- `window.open`、`target=_blank`、页面跳转和会话恢复的单 Tab 运行回归。
- HTTPS 反向代理及客户端多设备同步、冲突恢复运行测试；服务端 PostgreSQL 集成验证已通过。
- IndexedDB、Cache Storage、Service Worker 的同步范围和一致性实现。
- Android 独立 SDK/NDK 构建输出、已编码消息宿主和 JNI 的完整 Java/C++ 编译、Tab/存储/指纹/SAF 文件/原生 HTTP/Keystore 接入、APK/AAB 和设备回归；已完成的 JVM 与端口互通不代表这些平台项完成。
- Development 构建图与缓存恢复、统一构建和重新链接；PC 新 Chrome 尚未链接，单文件编译、局部语法检查和干跑不能作为新运行版验收。
- Release 全量编译、发布目录、安装升级和整体回归测试。

## 编译与变更规则

- 日常修改优先局部静态检查或目标编译，不主动执行 GN 重生成。
- 预计引发大范围重新编译的 GN 或公共配置变更，先说明影响并取得许可。
- 所有功能编码完成后，统一进行必要的 Release 批量编译和整体测试。
- `build/src` 为本地 Chromium 源码输出目录；可持久化的 Chromium 修改必须通过 `patches/series` 重放。

## 已知独立问题

`disable-gcm.patch` 已修正 1 处 hunk 行数，Git 格式解析通过；`build-compatibility.patch` 已修正 15 处行数，补丁读取检查通过，但仍有 GPU 和 Blink 两段纯上下文块使 Git 完整格式解析失败。修正只涉及 hunk 计数，没有修改正文或 GN。历史核对确认 Blink 段为撤销依赖后遗留的上下文，GPU 段首次提交即缺少增删标记，暂未猜测修复或删除；从净源码完整重放仍待验证。

此前 Development 前端局部目标在 Ninja 自动重生成时因工具链发现失败而停止。已核实 Visual Studio 2022 和 Windows SDK 10.0.26100.0 分别安装在 D 盘 Visual Studio 目录与 `D:\Windows Kits\10`；它们并未缺失。局部验证应复用已有工具路径，避免通过自动 GN 重生成改变构建图。

本轮核对 `build/src/out/Development/build.ninja` 仅 558 字节，只包含 GN 重生成入口，未包含完整目标构建图。此前临时 driver 对 `chrome.dll` 的干跑列出 54340 个待执行步骤，这些步骤没有执行，PC 新 Chrome 没有完成链接。当前状态应记为“构建缓存/构建图恢复及发布统一构建未完成”，不能归因为 Windows SDK 缺失，也不能将干跑步骤数记为已完成编译。限流任务的 CGO 编译器缺口属于 Go 竞态检测环境，与上述 Chromium 构建状态不同。

Android 环境另有独立限制：WSL 返回 `HCS_E_HYPERV_NOT_INSTALLED`，现有 Linux 发行版不能启动。本轮没有擅自修改系统功能或重启；先完成可独立验证的编码和测试。生产地址仍需设为设备可访问的实际 SaaS 服务，不能把 Android 的本机回环地址当成 PC 服务地址。

## 下一步顺序

1. 恢复 Development 构建图与缓存，评估实际编译范围后统一构建并重新链接，使最新 SaaS WebUI 和原生桥进入可运行二进制。
2. 运行白名单页面、普通页面、双 Tab 存储隔离、文件读写和 HTTP 桥接测试。
3. 完成单 Tab 跳转、快照恢复和 SaaS 服务端真实数据库集成验证。
4. 完成 Android 工具链和平台适配后，再做移动端构建。
5. 所有功能验收后执行 Release 全量编译、打包和整体回归。
