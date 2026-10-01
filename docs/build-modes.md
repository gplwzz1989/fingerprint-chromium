# 构建模式

项目固定使用两套 GN 参数，开发验证和发版编译不共用输出目录。

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
$src = 'F:\mywork\chrome-finger\build\src'
$out = Join-Path $src 'out\Development'
New-Item -ItemType Directory -Force -Path $out | Out-Null
Copy-Item 'F:\mywork\chrome-finger\build-configs\development.gn' (Join-Path $out 'args.gn') -Force
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
$src = 'F:\mywork\chrome-finger\build\src'
$out = Join-Path $src 'out\Release'
Copy-Item 'F:\mywork\chrome-finger\build-configs\release.gn' (Join-Path $out 'args.gn') -Force
& (Join-Path $out 'gn.exe') gen $out
```

注意：首次切换模式或修改 GN 参数前，必须先说明构建图变化和预计成本；日常开发不得因为小改动自动切换到 Release 或重新生成 GN。
