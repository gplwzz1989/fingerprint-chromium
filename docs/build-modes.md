# 构建模式

当前 Windows PC 构建固定使用两套 GN 参数，开发验证和发版编译不共用输出目录。`common.gn` 含 Windows 工具链路径和 x64 目标，不直接作为 Android 构建配置；Android 须使用独立平台参数、ABI 和输出，不能覆盖这里的 PC 输出。两端定位、构建与验收边界见 [项目总览第 5.4 节](../PROJECT-OVERVIEW.md#54-pc-与-android-的分工差异和联动)。

PC 与 Android 的输出、临时目录、依赖缓存和平台工具路径统一记录在 Git 跟踪的 `build-configs/build-roots.json`。当前 Android 只登记 ARM64 配置和 D 盘预定路径，不代表 SDK/NDK 已安装，也不创建 Android 输出或触发编译。

所有编译参数受根目录 `AGENTS.md` 的强制锁约束。下文配置准备、模式切换和 GN 命令仅为操作参考，不构成修改许可；覆盖现有参数前必须报告具体差异和预计重编译源文件/编译单元数量，并取得用户本次确认。已存在的模板与实际 `args.gn` 差异不得自动统一，配置批准也不代替全量编译批准。

## 开发验证模式

配置文件：`build-configs/development.gn`

- `is_official_build = false`
- `is_component_build = true`
- `use_thin_lto = false`
- `dcheck_always_on = true`
- 符号级别为 0

用途：日常功能开发、局部目标编译、增量编译和运行验证。该模式不用于最终打包。

在 Windows PowerShell 中初始化开发输出目录：

```powershell
$repo = 'F:\mywork\chrome-finger'
$src = Join-Path $repo 'build\src'
& (Join-Path $repo 'utils\prepare_build_mode.ps1') -Mode development
$out = Join-Path $src 'out\Development'
& (Join-Path $src 'out\Release\gn.exe') gen $out
```

局部验证示例：

```powershell
& 'D:\codex-tools\build-tools\ninja-1.11\bin\ninja.exe' -C $out chrome/browser/ui/ui/browser_commands.obj
```

## Release 发版模式

配置文件：`build-configs/release.gn`

- `is_official_build = true`
- `is_component_build = false`
- `use_thin_lto = true`
- `thin_lto_enable_optimizations = true`
- `enable_widevine = false`
- 符号级别为 0

用途：所有功能开发和开发验证完成后的整体编译、运行回归、打包和发布。

初始化或更新 Release 输出目录：

```powershell
$repo = 'F:\mywork\chrome-finger'
$src = Join-Path $repo 'build\src'
& (Join-Path $repo 'utils\prepare_build_mode.ps1') -Mode release
$out = Join-Path $src 'out\Release'
& (Join-Path $out 'gn.exe') gen $out
```

注意：首次切换模式或修改 GN 参数前，必须先说明构建图变化和预计成本；日常开发不得因为小改动自动切换到 Release 或重新生成 GN。

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
