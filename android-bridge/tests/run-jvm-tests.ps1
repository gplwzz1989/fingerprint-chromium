param(
    [string]$OutputDirectory = '',
    [string]$JdkDirectory = 'D:\Program Files\Eclipse Adoptium\jdk-17.0.18.8-hotspot',
    [string]$NodeExecutable = 'D:\Program Files\nodejs\node.exe',
    [string]$KotlinCompilerDirectory = ''
)
$ErrorActionPreference = 'Stop'
if (-not $OutputDirectory) {
    $OutputDirectory = 'D:\tmp\android-bridge-crypto-' + [guid]::NewGuid().ToString('N')
}
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
if (-not $OutputDirectory.StartsWith('D:\', [StringComparison]::OrdinalIgnoreCase)) {
    throw '测试输出目录必须位于 D 盘'
}
$JdkDirectory = [IO.Path]::GetFullPath($JdkDirectory)
if (-not $JdkDirectory.StartsWith('D:\', [StringComparison]::OrdinalIgnoreCase)) {
    throw '请使用 D 盘已有的 JDK'
}
$javac = Join-Path $JdkDirectory 'bin\javac.exe'
$java = Join-Path $JdkDirectory 'bin\java.exe'
if (-not (Test-Path -LiteralPath $javac) -or -not (Test-Path -LiteralPath $NodeExecutable)) {
    throw '未找到指定的 JDK 或 Node，请设置已有工具路径'
}
$bridgeRoot = Split-Path -Parent $PSScriptRoot
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null
$sources = @(Get-ChildItem -LiteralPath (Join-Path $bridgeRoot 'src\main\java'),
    (Join-Path $bridgeRoot 'src\test\java') -Recurse -Filter '*.java' | ForEach-Object FullName)
& $javac "-J-Djava.io.tmpdir=$OutputDirectory" --release 8 -encoding UTF-8 -Xlint:all -d $OutputDirectory @sources
if ($LASTEXITCODE -ne 0) { throw 'Java 适配器编译失败' }
& $java "-Djava.io.tmpdir=$OutputDirectory" '-Dfile.encoding=UTF-8' -cp $OutputDirectory com.fingerprint.saas.bridge.SnapshotCryptoSelfTest
if ($LASTEXITCODE -ne 0) { throw 'JVM 自测失败' }
foreach ($testClass in @('SaasOriginPolicySelfTest', 'SaasBridgeDispatcherSelfTest', 'SaasHttpClientSelfTest')) {
    & $java "-Djava.io.tmpdir=$OutputDirectory" '-Dfile.encoding=UTF-8' -cp $OutputDirectory "com.fingerprint.saas.bridge.$testClass"
    if ($LASTEXITCODE -ne 0) { throw '原生桥安全与消息路由自测失败' }
}
& $NodeExecutable (Join-Path $PSScriptRoot 'snapshot-interop.cjs') $java $OutputDirectory
if ($LASTEXITCODE -ne 0) { throw 'Node／JVM 交叉兼容验证失败' }
& $NodeExecutable (Join-Path $PSScriptRoot 'port-interop.cjs') $java $OutputDirectory
if ($LASTEXITCODE -ne 0) { throw '原生端口／网页／JVM 消息互操作验证失败' }
if ($KotlinCompilerDirectory) {
    $KotlinCompilerDirectory = [IO.Path]::GetFullPath($KotlinCompilerDirectory)
    if (-not $KotlinCompilerDirectory.StartsWith('D:\', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Kotlin 编译器必须位于 D 盘'
    }
    $stdlib = Join-Path $KotlinCompilerDirectory 'lib\kotlin-stdlib.jar'
    if (-not (Test-Path -LiteralPath $stdlib)) { throw '未找到指定的 Kotlin 编译器' }
    $kotlinSources = @(Get-ChildItem -LiteralPath (Join-Path $bridgeRoot 'src\main\kotlin'),
        (Join-Path $bridgeRoot 'src\test\kotlin') -Recurse -Filter '*.kt' | ForEach-Object FullName)
    & $java "-Djava.io.tmpdir=$OutputDirectory" "-Dkotlin.home=$KotlinCompilerDirectory" '-Dfile.encoding=UTF-8' `
        -cp (Join-Path $KotlinCompilerDirectory 'lib\*') org.jetbrains.kotlin.cli.jvm.K2JVMCompiler `
        -no-reflect -no-stdlib -classpath "$OutputDirectory;$stdlib" -jvm-target 1.8 -d $OutputDirectory @kotlinSources
    if ($LASTEXITCODE -ne 0) { throw 'Kotlin 契约与适配器编译失败' }
    & $java "-Djava.io.tmpdir=$OutputDirectory" '-Dfile.encoding=UTF-8' -cp "$OutputDirectory;$stdlib" `
        com.fingerprint.saas.bridge.SnapshotCryptoAdapterSelfTest
    if ($LASTEXITCODE -ne 0) { throw 'Kotlin 适配器自测失败' }
} else {
    Write-Output '未指定 Kotlin 编译器目录；本次仅验证 Java 核心及网页互操作'
}
Write-Output "编译与验证完成，输出目录：$OutputDirectory"
