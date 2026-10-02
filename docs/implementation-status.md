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
- 桌面原生桥：Tab、存储、指纹、`SaasFiles` 专用目录文件读写和自定义 HTTP 请求。
- Android 平台无关桥接契约，后续使用 Android Storage Access Framework、Keystore 和原生网络适配。
- 构建模式、同步边界、桥接消息、部署方式和测试结果文档。

## 已完成的局部验证

- `saas-server`：`go test ./...`、`go vet ./...` 通过。
- `saas-web`：Node JavaScript 语法检查通过。
- Chromium WebUI：TypeScript 静态检查通过。
- 原生桥：`management_ui_handler.cc` 使用现有开发版编译参数完成 C++ 局部语法编译。
- 新增 SaaS 桥接补丁可通过反向应用检查，并已加入 `patches/series`。
- 指纹配置编辑表单已在现有指纹管理器 WebUI 中实现，包含 User-Agent、硬件并发数和指纹种子输入；尚未在包含最新源码的运行版中回归。
- 未重新生成 GN，未启动 Release 全量编译。

## 尚未完成

- 最新源码尚未重新链接进入可展示 SaaS 页面的新开发版二进制；当前测试版仍是旧的 Development 输出。
- 独立 SaaS Web 在 Chromium 开发版中的白名单桥接、文件和 HTTP 运行联调。
- `window.open`、`target=_blank`、页面跳转和会话恢复的单 Tab 运行回归。
- 真实 PostgreSQL、HTTPS 反向代理、多设备同步和冲突恢复测试。
- IndexedDB、Cache Storage、Service Worker 的同步范围和一致性实现。
- Android SDK/NDK 准备、原生实现、APK/AAB 构建和移动端回归。
- Release 全量编译、发布目录、安装升级和整体回归测试。

## 编译与变更规则

- 日常修改优先局部静态检查或目标编译，不自动重新生成 GN。
- 预计引发大范围重新编译的 GN 或公共配置变更，先说明影响并取得许可。
- 所有功能编码完成后，统一进行必要的 Release 批量编译和整体测试。
- `build/src` 为本地 Chromium 源码输出目录；可持久化的 Chromium 修改必须通过 `patches/series` 重放。

## 已知独立问题

`patches/core/ungoogled-chromium/disable-gcm.patch` 和 `patches/upstream-fixes/build-compatibility.patch` 存在历史格式问题。本阶段未自动重写它们，避免在没有基线文件和语义确认的情况下改变原有补丁行为。

本次 Development 前端局部目标未进入编译：Ninja 自动检查到旧构建目录需要重新生成，D 盘 Visual Studio 2022 可以被显式发现，但本机未发现 Windows SDK，因此工具链预检失败。未因此修改 GN、安装系统盘工具或启动全量编译。

## 下一步顺序

1. 重新编译开发版必要目标，使最新 SaaS WebUI 和原生桥进入可运行二进制。
2. 运行白名单页面、普通页面、双 Tab 存储隔离、文件读写和 HTTP 桥接测试。
3. 完成单 Tab 跳转、快照恢复和 SaaS 服务端真实数据库集成验证。
4. 完成 Android 工具链和平台适配后，再做移动端构建。
5. 所有功能验收后执行 Release 全量编译、打包和整体回归。
