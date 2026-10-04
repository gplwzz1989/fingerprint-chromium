# 构建模式

当前 Windows PC 仅保留 Release 构建配置，局部验证与发版均使用现有 `out/Release`。Development 模板已移入回收站，模式准备入口和活跃路径配置已移除；原 Development 输出、`args.gn`、对象及依赖缓存独立保留，不覆盖为 Release。`common.gn` 含 Windows 工具链路径和 x64 目标，不直接作为 Android 构建配置；Android 须使用独立平台参数、ABI 和输出，不能覆盖这里的 PC 输出。两端定位、构建与验收边界见 [项目总览第 5.4 节](../PROJECT-OVERVIEW.md#54-pc-与-android-的分工差异和联动)。

PC 与 Android 的输出、临时目录、依赖缓存和平台工具路径统一记录在 Git 跟踪的 `build-configs/build-roots.json`。Android ARM64 活跃输出为 WSL 的 `/home/gaoyang/chromium-build/AndroidDevelopment`，Linux GN/Ninja/Clang/Rust/bindgen、SDK/NDK 与实际 JDK 已显式登记。旧 `build/src/out/AndroidArm64` 不作为活跃入口，后续使用本文的 WSL 检查入口；实际单源文件编译通过不代替完整 APK、链接或设备验收。

所有编译参数受根目录 `AGENTS.md` 的强制锁约束。下文配置准备、模式切换和 GN 命令仅为操作参考，不构成修改许可；覆盖现有参数前必须报告具体差异和预计重编译源文件/编译单元数量，并取得用户本次确认。已存在的模板与实际 `args.gn` 差异不得自动统一，配置批准也不代替全量编译批准。

Chromium 修复优先在发生问题的源码、目标、平台实现或补丁内局部处理。不得为绕过局部编译/链接/运行错误擅自调整全局宏、警告/检查、功能开关、优化/链接/组件模式或工具链。确需全局或编译参数变更时，先说明局部方案为何不足及变更原因、参数差异和预计重编译文件量，得到用户针对本次方案的明确确认后才能修改；PC 与 Android 相同。

Git 同时跟踪 Release 配置模板、`build/src/out/Release/args.gn` 和 `build/src/build-configs/common.gn`。仅这两个现有小文件例外加入；Development 的实际 `args.gn` 已停止跟踪但仍保留在磁盘，其历史内容可从 Git 查询；`build/` 的其他源码、构建图、对象和缓存继续忽略。Android 的 Git 模板与 WSL 活跃输出实际 `args.gn` 本次已同步为相同内容；后续仍须检查差异并遵守参数锁，不将 Linux 输出、对象或缓存提交到 Git。

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
- 活跃输出为 `/home/gaoyang/chromium-build/AndroidDevelopment`；旧 `build/src/out/AndroidArm64` 保留但不作为后续构建入口
- 模板和活跃 `args.gn` 均不导入 Windows PC 的 `common.gn`，原导入中的生效值已独立登记，功能和优化参数保持不变
- WSL Linux SDK 为 `/home/gaoyang/Android/Sdk`，平台版本 36、构建工具 36.0.0，NDK 为 28.0.13004108（r28）
- Chromium Java 编译固定调用 `build/src/third_party/jdk/current` 中的 Linux JDK 23.0.2；`build-roots.json` 已登记相同 WSL 路径
- 活跃输出使用 Linux `nightly-2025-11-12` Rust（1.93.0-nightly）及 `/home/gaoyang/chromium-tools/rust-toolchain-fixed` 中的 Linux bindgen。前者已有 Android ARM64 预编译标准库；后者的 Chromium Rust 包没有该标准库，不能仅切换 `rust_sysroot_absolute` 到后者。自定义 Rust 是源码支持的配置路径，但仍须实际平台编译和运行验收
- 活跃图使用 `/home/gaoyang/chromium-tools/gn`；旧 Android 图引用 PC `out/Release/gn_build/gn`，不得继续以 PC GN 子目标输出构建 Linux GN。历史混合输出原样保留，不清理或覆盖

Android 构建必须从 WSL Linux 仓库路径执行，不能从 Windows PowerShell 使用 `gn.exe`、`ninja.exe`、Windows SDK/NDK/JDK 或 PC 输出。开始前先确认 WSL 发行版、Linux 工具链、SDK/NDK/JDK、构建图和独立缓存；缺失时停止在环境检查。

`utils/prepare_build_mode.ps1 -Mode android-arm64` 现在在任何目录创建和配置同步前拒绝执行，避免再次向旧输出写入缺少 Linux Rust 路径的模板。Windows PowerShell 仅负责 WSL 状态检查和结果查看；后续 Android 配置维护、GN/Ninja 均在 WSL 内针对已登记活跃输出执行，参数确认前不复制模板、不重生成图。PC Release 配置准备入口保持原行为。

### 2026-10-04 已确认的 Android 配置修复（2026-10-05 验证）

根因是 Git 模板未记录活跃输出的 Linux Clang/Rust/bindgen 和 Android 参数，配置入口又固定写入旧目录；JDK 清单与 Chromium 固定调用路径也不一致。用户已针对以下方案明确同意修改；仅实施所列路径和独立配置修复，不切换实际工具链或优化/功能参数。

| 文件/项目 | 旧值 | 已实施变更 |
| --- | --- | --- |
| `android-arm64.gn` 的 Clang 路径 | 未显式记录，依赖源码目录链接 | 显式记录 `/home/gaoyang/chromium-tools/clang`，与活跃输出一致 |
| 模板的 Rust/bindgen | 未记录 Linux 自定义工具链，默认指向 Windows 包 | 显式记录当前 `rust_sysroot_absolute=/home/gaoyang/.rustup/toolchains/nightly-2025-11-12-x86_64-unknown-linux-gnu`、`rust_bindgen_root=/home/gaoyang/chromium-tools/rust-toolchain-fixed` 和 `rustc_version=rustc 1.93.0-nightly (25d319a0f 2025-11-11)` |
| 活跃 `args.gn` 的 PC 配置导入 | `import("//build-configs/common.gn")` | 移除导入，将其实际生效的项目参数显式保存在独立 Android 模板/参数中；保留当前 `arm_control_flow_integrity=pac`、`dcheck_always_on=true`、SDK 36/36.0.0、NDK API 35、`pdf_enable_rust_png=false`、`android_static_analysis=off`、`use_errorprone_java_compiler=false`、AAPT2 路径与所有其余生效值，不恢复或扩大功能 |
| `build-roots.json` 的 `java_home` | `/usr/lib/jvm/java-17-openjdk-amd64` | `/mnt/f/mywork/chrome-finger/build/src/third_party/jdk/current`（实际 Linux JDK 23.0.2）；登记实际 GN/Ninja/Clang/Rust/bindgen 路径用于 WSL 入口检查 |
| 后续配置维护入口 | PowerShell 固定写旧 `out/AndroidArm64` | 提供 WSL 内入口，读取登记的现有输出，默认只检查差异，显式更新时按内容比较写入，不运行 GN/Ninja、不改变环境变量 |

影响平台仅 Android ARM64，继续使用 `/home/gaoyang/chromium-build/AndroidDevelopment` 的现有缓存；PC Release、历史 PC/Android 输出均不更改。计划保留全部实际生效值，预计本方案新增 C/C++ 源文件重编译 0、Rust 编译单元重编译 0、编译命令变更导致的链接/归档重做 0；移除 GN 导入后需要一次构建图重生成验证，不能把此估算当作现有构建待执行量为零。只读 `ninja -t commands chrome_public_apk` 得到基线：36,768 个不同 C/C++ 源文件、41,768 个 C/C++ 编译单元、173 个 Rust 编译单元（115 个根源文件）、11 个 bindgen 动作、12,439 个其他生成动作、2,130 个归档动作、26 个链接动作。统计是完整目标范围，不是增量干跑；若确认后发现命令或目标范围变化，停止并重新估算，不自动继续构建。

本方案不确认既有功能/静态检查关闭的必要性；仅保留当前构建行为，禁止以模板修复为由新增关闭项。启用这些检查或恢复 Rust PNG 应另列参数差异、影响范围和验收结果。Linux GN 已有独立 `/home/gaoyang/chromium-tools/gn`，后续入口不再引用 PC GN 子目标；现存混合缓存保持原样，不在本方案中清理或重建。

后续在 WSL 内执行配置检查：

```bash
cd /mnt/f/mywork/chrome-finger
python3 utils/prepare_android_build.py
```

此入口验证已登记的 Linux 可执行文件、版本、SDK/NDK、Rust Android 标准库、构建图的 GN/源码根目录及模板一致性。以后参数变更经明确确认后，可用 `python3 utils/prepare_android_build.py --apply` 原地同步；内容不变不写入，不改变环境变量，也不自动执行 GN 或编译。

本次原地 GN 重生成成功（57,663 个目标、4,068 个输入文件）。前后 1,203 项生效参数与 56,547 条完整构建命令完全相同，命令图 SHA-256 为 `e22394ba302831fd3628a793c90eb967f075bbe01a5569f681153b33b793144f`，验证本次配置修复没有新增命令变化导致的源码重编译。使用 Ninja 图中的完整 Android ARM64/API35 参数实际编译 `base/check.cc` 成功，独立验证产物为同一输出对象目录内的 `check.toolchain-validation.o`（30,520 字节，AArch64 ELF），原对象不覆盖。本次证明工具链及实际源码编译可开始，不代表完整 APK、链接或设备验收通过。
