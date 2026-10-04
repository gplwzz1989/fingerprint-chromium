#Requires -Version 7.0
[CmdletBinding()]
param(
    [string]$OutputPath = 'output/saas/fingerprint-saas.zip'
)

$ErrorActionPreference = 'Stop'
$taskRepo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskStage = $null
$taskFailed = $false
$taskFailureMessage = 'SaaS 打包失败，请检查 Go 工具、资源文件、目标文件占用及回收站可用性。'

function Stop-Package([string]$Message) {
    $script:taskFailureMessage = $Message
    throw '打包验证失败'
}

function Move-ToRecycleBin([string]$Path) {
    # 使用 Windows 回收站保留恢复能力，不永久删除历史包或临时文件。
    if ([IO.Directory]::Exists($Path)) {
        [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory(
            $Path, 'OnlyErrorDialogs', 'SendToRecycleBin', 'ThrowException')
    } elseif ([IO.File]::Exists($Path)) {
        [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteFile(
            $Path, 'OnlyErrorDialogs', 'SendToRecycleBin', 'ThrowException')
    }
}

try {
    if (-not $IsWindows) {
        Stop-Package '打包脚本需要 Windows 回收站，请在 Windows 主机运行。'
    }
    Add-Type -AssemblyName Microsoft.VisualBasic.Core
    Add-Type -AssemblyName System.IO.Compression.ZipFile
    $taskTarget = [IO.Path]::GetFullPath($OutputPath, $taskRepo)
    if ([IO.Path]::GetExtension($taskTarget) -ine '.zip') {
        Stop-Package '请使用原有 ZIP 文件名，输出路径必须以 .zip 结尾。'
    }
    # 禁止把 SaaS 包写入源码或 Chromium 工作区，避免覆盖现有文件。
    $taskRepoPrefix = $taskRepo + [IO.Path]::DirectorySeparatorChar
    $taskOutputPrefix = (Join-Path $taskRepo 'output') + [IO.Path]::DirectorySeparatorChar
    if ($taskTarget.StartsWith($taskRepoPrefix, [StringComparison]::OrdinalIgnoreCase) -and
        -not $taskTarget.StartsWith($taskOutputPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        Stop-Package '项目内的 SaaS 包只能放在 output 目录，不能覆盖源码或 Chromium 产物。'
    }
    $taskParent = [IO.Path]::GetDirectoryName($taskTarget)
    # 拒绝经过链接/联接点的输出路径，防止间接进入受保护目录。
    $taskExistingParent = $taskParent
    while (-not (Test-Path -LiteralPath $taskExistingParent)) {
        $taskExistingParent = [IO.Path]::GetDirectoryName($taskExistingParent)
    }
    $taskAncestor = Get-Item -LiteralPath $taskExistingParent -Force
    while ($null -ne $taskAncestor) {
        if (($taskAncestor.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            Stop-Package '输出目录不能经过符号链接或目录联接点，请使用真实目录。'
        }
        $taskAncestor = $taskAncestor.Parent
    }
    [IO.Directory]::CreateDirectory($taskParent) | Out-Null
    if (Test-Path -LiteralPath $taskTarget) {
        $taskExisting = Get-Item -LiteralPath $taskTarget -Force
        if ($taskExisting.PSIsContainer -or
            ($taskExisting.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            Stop-Package '目标必须是普通 ZIP 文件，不能是目录或链接。'
        }
    }
    $taskGo = (Get-Command go -ErrorAction Stop).Source
    $taskGoOS = & $taskGo env GOOS
    if ($LASTEXITCODE -ne 0) { Stop-Package '读取 Go 目标平台失败。' }
    $taskExecutable = if ($taskGoOS -eq 'windows') { 'fingerprint-saas.exe' } else { 'fingerprint-saas' }
    $taskStage = Join-Path $taskParent ('.saas-package-' + [Guid]::NewGuid().ToString('N'))
    $taskBundle = Join-Path $taskStage 'bundle'
    [IO.Directory]::CreateDirectory((Join-Path $taskBundle 'saas-web')) | Out-Null
    Push-Location (Join-Path $taskRepo 'saas-server')
    try {
        & $taskGo build -trimpath -o (Join-Path $taskBundle $taskExecutable) ./cmd/fingerprint-saas
        if ($LASTEXITCODE -ne 0) { Stop-Package 'SaaS 服务编译失败，旧包未替换。' }
    } finally {
        Pop-Location
    }
    # 只收录真实运行资源，不打包测试探针、数据库、环境配置或 Chromium。
    $taskAssets = @('index.html', 'styles.css', 'app.js', 'administration.js',
        'operations.js', 'bridge-contract.js', 'session-persistence.js', 'snapshot-sync.js')
    foreach ($taskAsset in $taskAssets) {
        Copy-Item -LiteralPath (Join-Path $taskRepo "saas-web/$taskAsset") -Destination (Join-Path $taskBundle 'saas-web')
    }
    $taskReadme = [IO.File]::ReadAllText((Join-Path $taskRepo 'saas-server/README.md'))
    [IO.File]::WriteAllText((Join-Path $taskBundle 'README.md'),
        $taskReadme.Replace('../PROJECT-OVERVIEW.md', 'PROJECT-OVERVIEW.md'), [Text.UTF8Encoding]::new($false))
    Copy-Item -LiteralPath (Join-Path $taskRepo 'PROJECT-OVERVIEW.md') -Destination $taskBundle
    $taskArchive = Join-Path $taskStage 'package.zip'
    [IO.Compression.ZipFile]::CreateFromDirectory($taskBundle, $taskArchive)
    $taskCheck = [IO.Compression.ZipFile]::OpenRead($taskArchive)
    try {
        if ($taskCheck.Entries.Count -ne 11 -or $null -eq $taskCheck.GetEntry($taskExecutable)) {
            Stop-Package 'SaaS 归档内容检查失败，旧包未替换。'
        }
    } finally {
        $taskCheck.Dispose()
    }
    # 原子替换并保留旧包，再送回收站；替换失败时原目标仍保持可用。
    if ([IO.File]::Exists($taskTarget)) {
        $taskPreviousDirectory = Join-Path $taskStage 'previous'
        [IO.Directory]::CreateDirectory($taskPreviousDirectory) | Out-Null
        $taskPrevious = Join-Path $taskPreviousDirectory ([IO.Path]::GetFileName($taskTarget))
        [IO.File]::Replace($taskArchive, $taskTarget, $taskPrevious)
        Move-ToRecycleBin $taskPrevious
    } else {
        [IO.File]::Move($taskArchive, $taskTarget)
    }
    Write-Host "SaaS 打包完成：$taskTarget"
} catch {
    $taskFailed = $true
    Write-Host $taskFailureMessage
} finally {
    if ($null -ne $taskStage -and [IO.Directory]::Exists($taskStage)) {
        try {
            Move-ToRecycleBin $taskStage
        } catch {
            $taskFailed = $true
            Write-Host "临时目录未能移入回收站，请稍后处理：$taskStage"
        }
    }
}
if ($taskFailed) { exit 1 }
