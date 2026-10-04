param(
  [Parameter(Mandatory = $true)]
  [ValidateSet('development', 'release')]
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
$outName = if ($Mode -eq 'development') { 'Development' } else { 'Release' }
$outRoot = Join-Path $SourceRoot ('out\' + $outName)

foreach ($path in @($commonConfig, $modeConfig)) {
  if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
    throw "构建配置不存在：$path"
  }
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

$commonDestination = Join-Path $sourceConfigRoot 'common.gn'
$argsDestination = Join-Path $outRoot 'args.gn'
$commonChanged = Sync-ConfigFile -Source $commonConfig -Destination $commonDestination
$argsChanged = Sync-ConfigFile -Source $modeConfig -Destination $argsDestination

if ($commonChanged -or $argsChanged) {
  Write-Output "已更新 $Mode 构建配置：$outRoot"
} else {
  Write-Output "$Mode 构建配置未变化：$outRoot"
}
