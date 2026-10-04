# 项目任务边界

- 后续开发先阅读 `PROJECT-OVERVIEW.md`，统一了解目标、进度、模块关系及验证边界；阶段总结写入 `docs/implementation-status.md`，涉及整体状态时同步更新总览。
- 修改归属按总览第 5 节判断：页面和业务流程在 `saas-web/`，鉴权/权限/目录/密文版本/租约等在 `saas-server/`；原生 Tab、账号隔离、指纹/代理实际效果、系统能力及桥来源安全在指纹 Chromium / `android-bridge/`。已有桥能力足够时只改 SaaS，不扩展历史 WebUI 业务、不编译 Chromium。
- 新增或改变原生能力须联动 Web 桥契约、PC/Android 实现、能力声明与协议文档；未实现平台明确不可用。原生来源授权不能代替 Go 业务鉴权，服务端 CORS 也不能授予原生桥权限。
- Chromium 修改同步 `build/src/` 对应源码和可重放补丁，Android 实现与对应补丁保持一致，新增补丁登记 `patches/series`；源码/局部测试通过不等于最新二进制或设备验收通过。
- Chromium 平台修改按总览第 5.4 节判断：PC 原生窗口/WebUI/TabStrip 与 Android Java/TabModel/JNI/SAF/Keystore 分别定位；共享引擎、默认地址和桥契约评估两端影响，不凭补丁名判断。移动 SaaS 布局仍改 `saas-web/`；现有 Windows x64 GN/局部检查不代表 Android 配置或编译通过，Android 输出与验收独立且不能覆盖 PC 输出。
- 完整保留 `build/` 和 `publish/` 及 Chromium 源码、构建图、对象、生成文件、依赖记录和缓存，不通过清理触发大规模重新编译。
- SaaS 打包使用 `utils/package_saas.ps1`，保持既有 `-OutputPath` 文件名；成功生成新包后旧包移入回收站，覆盖同一路径，不累积日期版本。项目内 SaaS 归档放在 `output/`，不混入 Chromium 输出。
- 历史文件仅在确认无引用、非运行数据且非 Chromium 产物后移入回收站；禁止永久删除。

- 只有明确属于 Android 逆向、APK 分析、抓包或动态调试的任务，才读取 `C:\Users\Administrator\.codex\android-reverse-tools.md` 并使用逆向工具链。
- Android 构建、编译、GN/Ninja、SDK/NDK 配置、平台适配和测试不属于逆向任务，不读取逆向工具说明，也不调用 JADX、APKTool、Frida 或其他逆向流程。
