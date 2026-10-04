# 构建模式

当前 Windows PC 仅保留 Release 构建配置，局部验证与发版均使用现有 `out/Release`。Development 模板已移入回收站，模式准备入口和活跃路径配置已移除；原 Development 输出、`args.gn`、对象及依赖缓存独立保留，不覆盖为 Release。`common.gn` 含 Windows 工具链路径和 x64 目标，不直接作为 Android 构建配置；Android 须使用独立平台参数、ABI 和输出，不能覆盖这里的 PC 输出。两端定位、构建与验收边界见 [项目总览第 5.4 节](../PROJECT-OVERVIEW.md#54-pc-与-android-的分工差异和联动)。

PC 与 Android 的输出、临时目录、依赖缓存和平台工具路径统一记录在 Git 跟踪的 `build-configs/build-roots.json`。当前 Android 只登记 ARM64 配置和 D 盘预定路径，不代表 SDK/NDK 已安装，也不创建 Android 输出或触发编译。

所有编译参数受根目录 `AGENTS.md` 的强制锁约束。下文配置准备、模式切换和 GN 命令仅为操作参考，不构成修改许可；覆盖现有参数前必须报告具体差异和预计重编译源文件/编译单元数量，并取得用户本次确认。已存在的模板与实际 `args.gn` 差异不得自动统一，配置批准也不代替全量编译批准。

Chromium 修复优先在发生问题的源码、目标、平台实现或补丁内局部处理。不得为绕过局部编译/链接/运行错误擅自调整全局宏、警告/检查、功能开关、优化/链接/组件模式或工具链。确需全局或编译参数变更时，先说明局部方案为何不足及变更原因、参数差异和预计重编译文件量，得到用户针对本次方案的明确确认后才能修改；PC 与 Android 相同。

Git 同时跟踪 Release 配置模板、`build/src/out/Release/args.gn` 和 `build/src/build-configs/common.gn`。仅这两个现有小文件例外加入；Development 的实际 `args.gn` 已停止跟踪但仍保留在磁盘，其历史内容可从 Git 查询；`build/` 的其他源码、构建图、对象和缓存继续忽略。Android 目前只有 `build-configs/android-arm64.gn` 模板，没有实际输出参数，不伪造运行配置。

历史对比可使用 `git log -p -- build-configs build/src/out/Development/args.gn build/src/out/Release/args.gn build/src/build-configs/common.gn`。比较操作为只读；从 Git 恢复或覆盖实际参数仍须遵守强制锁，不能因存在历史版本而直接还原并触发重编译。

2026-10-04 用户已确认告警处理：共享 `default_warnings` 添加 C/C++ `-w` 和 Rust `-Awarnings`；PC 共用配置、Release 实际参数及 Android ARM64 模板均设为 `treat_warnings_as_errors=false`、`fatal_linker_warnings=false`。Java/D8/R8 与链接工具可保留非阻断告警输出，真正编译或链接错误仍失败。独立补丁为 `patches/upstream-fixes/ignore-compiler-warnings.patch`。本次只改配置与局部验证，未重生成 GN/Ninja 图；必须在对应图重生成后才会进入实际编译命令，不得直接篡改生成的 Ninja 文件。参数强制锁继续适用于其他变更。

## Release 发版模式

配置文件：`build-configs/release.gn`

- `is_official_build = true`
- `is_component_build = false`
- `use_thin_lto = true`
- `thin_lto_enable_optimizations = true`
- `enable_widevine = false`
- 符号级别为 0

用途：PC 日常局部验证、经确认的整体编译、运行回归、打包和发布。局部 C++ 检查工具默认读取现有 Release 构建规则；统一配置不代表自动授权全量构建。

初始化或更新 Release 输出目录：

```powershell
$repo = 'F:\mywork\chrome-finger'
$src = Join-Path $repo 'build\src'
& (Join-Path $repo 'utils\prepare_build_mode.ps1') -Mode release
$out = Join-Path $src 'out\Release'
& (Join-Path $out 'gn.exe') gen $out
```

注意：修改 GN 参数前，必须先说明构建图变化和预计成本；日常开发不得因为小改动自动重新生成 GN 或启动 Release 全量构建。模板与实际 `args.gn` 的其他差异继续保留，本次移除开发版配置不修改 Release 参数值。

## Android ARM64 配置

配置文件：`build-configs/android-arm64.gn`

- 独立 `target_os = "android"`、`target_cpu = "arm64"`
- 独立 `out/AndroidArm64` 输出目录
- 不导入 Windows PC 的 `common.gn`
- 独立 D 盘 SDK、NDK、JDK、临时目录和缓存路径见 `build-configs/build-roots.json`

仅准备配置文件时可使用：

```powershell
& (Join-Path $repo 'utils\prepare_build_mode.ps1') -Mode android-arm64
```

该命令只创建配置目录并复制 `args.gn`，不会执行 `gn gen`、Ninja、SDK/NDK 安装或 APK 编译。Android 完整构建须另行确认工具链、ABI 和设备验收范围。
