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
Copy-Item -LiteralPath $commonConfig -Destination (Join-Path $sourceConfigRoot 'common.gn') -Force
Copy-Item -LiteralPath $modeConfig -Destination (Join-Path $outRoot 'args.gn') -Force

Write-Output "已准备 $Mode 构建模式：$outRoot"
