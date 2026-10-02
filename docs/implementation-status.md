# 指纹 SaaS 浏览器阶段总结

## 当前阶段

本阶段完成了独立 SaaS Web 架构、桌面原生能力桥和 Android 契约的编码工作。SaaS 业务前端与后端位于独立项目中，Chromium 只负责受 Origin 白名单保护的本地能力，不承载登录、工作区、账号目录、同步和计费业务。

## 已完成

- 开发版和 Release 双编译配置：开发版启用组件化编译，Release 关闭组件化编译；Release 保留 ThinLTO，并关闭 Widevine。
- 现有受管理 Tab 的单父窗口模型、独立持久化 StoragePartition、账号级代理、User-Agent、UA-CH、硬件并发数和指纹种子。
- Cookie、LocalStorage、SessionStorage 的读取、写入、快照导入和导出链路。
- 独立 `saas-web/` 前端：真实登录、工作区、账号目录和 `window.saasBridge` 客户端封装；普通浏览器不使用演示数据或假接口。
- 独立 `saas-server/` 后端：用户会话、工作区、成员权限、账号目录、加密快照、版本冲突、租约、审计、静态 Web 托管和 API CORS 预检。
- 编译时完整 Origin 白名单：协议、主机和端口精确匹配，普通来源不能调用原生桥。
- 常驻 SaaS 启动规则：PC 自动打开固定宿主页，Android 原生 Tab 初始化完成后打开控制台；关闭单页和批量关闭均保留控制台，应用退出正常释放。
- PC 和 Android 共用 `chrome/common/chrome_switches.cc` 中的默认 SaaS 地址，后续改地址只需重编译该文件并重新链接对应平台；默认来源权限随常量更新，不改 GN。
- 桌面原生桥：Tab、存储、指纹、`SaasFiles` 专用目录文件读写和自定义 HTTP 请求。
- HTTP 页面加密兼容：无 WebCrypto 时调用受同一白名单保护的原生 PBKDF2/AES-GCM 接口；密码只存于当前页面内存，仍需运行版联调。
- Android 平台无关桥接契约，后续使用 Android Storage Access Framework、Keystore 和原生网络适配。
- 构建模式、同步边界、桥接消息、部署方式和测试结果文档。

## 已完成的局部验证

- `saas-server`：`go test ./...`、`go vet ./...` 通过。
- `saas-web`：Node JavaScript 语法检查通过。
- Chromium WebUI：TypeScript 静态检查通过。
- 独立 Web 加密同步：真实 PBKDF2/AES-GCM 往返、账号绑定、错误密码拒绝、同步类别过滤及旧 WebUI 信封兼容测试通过。
- 真实 PostgreSQL 集成：隔离测试实例下的初始化、登录、跨设备快照读取、并发租约、版本冲突、覆盖审计和会话撤销测试通过。
- 常驻控制台：六个桌面 C++ 源文件局部语法编译、TypeScript 检查、Android JNI 生成和新增补丁反向应用检查通过。
- 默认地址常量所在 `chrome_switches.cc` 已使用现有 Development 参数完成单文件对象编译，未调用 GN；还需要重链接后才能更新运行版。
- 原生桥：`management_ui_handler.cc` 使用现有开发版编译参数完成 C++ 局部语法编译。
- 新增 SaaS 桥接补丁可通过反向应用检查，并已加入 `patches/series`。
- 指纹配置编辑表单已在现有指纹管理器 WebUI 中实现，包含 User-Agent、硬件并发数和指纹种子输入；尚未在包含最新源码的运行版中回归。
- 未主动修改或执行 GN，未启动 Release 全量编译；局部 Ninja 目标在构建前自动尝试重生成时因工具链预检失败而停止。

## 尚未完成

- 最新源码尚未重新链接进入可展示 SaaS 页面的新开发版二进制；当前测试版仍是旧的 Development 输出。
- 独立 SaaS Web 在 Chromium 开发版中的白名单桥接、文件和 HTTP 运行联调。
- `window.open`、`target=_blank`、页面跳转和会话恢复的单 Tab 运行回归。
- HTTPS 反向代理及客户端多设备同步、冲突恢复运行测试；服务端 PostgreSQL 集成验证已通过。
- IndexedDB、Cache Storage、Service Worker 的同步范围和一致性实现。
- Android SDK/NDK 准备、原生实现、APK/AAB 构建和移动端回归。
- Release 全量编译、发布目录、安装升级和整体回归测试。

## 编译与变更规则

- 日常修改优先局部静态检查或目标编译，不主动执行 GN 重生成。
- 预计引发大范围重新编译的 GN 或公共配置变更，先说明影响并取得许可。
- 所有功能编码完成后，统一进行必要的 Release 批量编译和整体测试。
- `build/src` 为本地 Chromium 源码输出目录；可持久化的 Chromium 修改必须通过 `patches/series` 重放。

## 已知独立问题

`patches/core/ungoogled-chromium/disable-gcm.patch` 和 `patches/upstream-fixes/build-compatibility.patch` 存在历史格式问题。本阶段未自动重写它们，避免在没有基线文件和语义确认的情况下改变原有补丁行为。

此前 Development 前端局部目标在 Ninja 自动重生成时因工具链发现失败而停止。已核实 Visual Studio 2022 和 Windows SDK 10.0.26100.0 分别安装在 D 盘 Visual Studio 目录与 `D:\Windows Kits\10`；它们并未缺失。局部验证应复用已有工具路径，避免通过自动 GN 重生成改变构建图。

## 下一步顺序

1. 重新编译开发版必要目标，使最新 SaaS WebUI 和原生桥进入可运行二进制。
2. 运行白名单页面、普通页面、双 Tab 存储隔离、文件读写和 HTTP 桥接测试。
3. 完成单 Tab 跳转、快照恢复和 SaaS 服务端真实数据库集成验证。
4. 完成 Android 工具链和平台适配后，再做移动端构建。
5. 所有功能验收后执行 Release 全量编译、打包和整体回归。
