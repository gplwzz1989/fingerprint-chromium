param(
  [Parameter(Mandatory = $true)]
  [ValidateSet('release', 'android-arm64')]
  [string] $Mode,

  [string] $SourceRoot = ''
)

$ErrorActionPreference = 'Stop'

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (-not $SourceRoot) {
  $SourceRoot = Join-Path $repositoryRoot 'build\src'
}
$SourceRoot = (Resolve-Path $SourceRoot).Path

$configRoot = Join-Path $repositoryRoot 'build-configs'
$commonConfig = Join-Path $configRoot 'common.gn'
$modeConfig = Join-Path $configRoot ($Mode + '.gn')
$sourceConfigRoot = Join-Path $SourceRoot 'build-configs'
$outName = switch ($Mode) {
  'release' { 'Release'; break }
  'android-arm64' { 'AndroidArm64'; break }
}
$outRoot = Join-Path $SourceRoot ('out\' + $outName)

foreach ($path in @($modeConfig)) {
  if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
    throw "构建配置不存在：$path"
  }
}
if ($Mode -ne 'android-arm64' -and -not (Test-Path -LiteralPath $commonConfig -PathType Leaf)) {
  throw "构建配置不存在：$commonConfig"
}

New-Item -ItemType Directory -Force -Path $sourceConfigRoot | Out-Null
New-Item -ItemType Directory -Force -Path $outRoot | Out-Null

function Sync-ConfigFile {
  param(
    [Parameter(Mandatory = $true)][string] $Source,
    [Parameter(Mandatory = $true)][string] $Destination
  )

  if (Test-Path -LiteralPath $Destination -PathType Leaf) {
    $sourceHash = (Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash
    $destinationHash = (Get-FileHash -LiteralPath $Destination -Algorithm SHA256).Hash
    if ($sourceHash -eq $destinationHash) {
      return $false
    }
  }
  Copy-Item -LiteralPath $Source -Destination $Destination -Force
  return $true
}

$argsDestination = Join-Path $outRoot 'args.gn'
$commonChanged = $false
if ($Mode -ne 'android-arm64') {
  $commonDestination = Join-Path $sourceConfigRoot 'common.gn'
  $commonChanged = Sync-ConfigFile -Source $commonConfig -Destination $commonDestination
}
$argsChanged = Sync-ConfigFile -Source $modeConfig -Destination $argsDestination

if ($commonChanged -or $argsChanged) {
  Write-Output "已更新 $Mode 构建配置：$outRoot"
} else {
  Write-Output "$Mode 构建配置未变化：$outRoot"
}
