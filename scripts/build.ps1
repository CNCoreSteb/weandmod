# We&Mod 打包脚本(交互式)
# 用法:
#   .\build.ps1                    交互菜单
#   .\build.ps1 -Mode Release      直接打 Release 包(CI 用)
#   .\build.ps1 -Mode Debug        调试包(保留符号+控制台)
#   .\build.ps1 -Mode Check        只 vet + 编译检查,不产出 exe
param(
    [ValidateSet('Release','Debug','Check')]
    [string]$Mode,
    [string]$Version,
    [string]$Out = 'WeAndMod.exe'
)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)

function Find-Tool([string]$name, [string]$fallbackGlob) {
    $cmd = Get-Command $name -ErrorAction SilentlyContinue
    if ($cmd) { return }
    $cand = Get-Item $fallbackGlob -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($cand) { $env:PATH = "$($cand.DirectoryName);$env:PATH"; return }
    throw "未找到 $name,请先安装或加入 PATH"
}

if (-not $Mode) {
    Write-Host "`n=== We&Mod 打包 ===" -ForegroundColor Cyan
    Write-Host ' [1] Release  单文件 exe · 无控制台 · 去符号(推荐)'
    Write-Host ' [2] Debug    带控制台输出 · 保留调试符号'
    Write-Host ' [3] Check    仅 vet + 编译检查,不产出 exe'
    Write-Host ' [0] 取消'
    switch (Read-Host '选择') {
        '1' { $Mode = 'Release' }
        '2' { $Mode = 'Debug' }
        '3' { $Mode = 'Check' }
        default { Write-Host '已取消'; exit 0 }
    }
}

# 工具链:go / gcc(Fyne 需要 CGO);不在 PATH 时回退到常见安装位置
Find-Tool 'go'  'D:\DevTools\tools\*\go\bin\go.exe'
Find-Tool 'gcc' "$env:LOCALAPPDATA\Microsoft\WinGet\Packages\BrechtSanders.WinLibs*\mingw64\bin\gcc.exe"
$env:CGO_ENABLED = '1'

Write-Host "==> go vet" -ForegroundColor Cyan
go vet ./...
if ($LASTEXITCODE) { throw 'go vet 未通过' }

if ($Mode -eq 'Check') {
    go build ./...
    if ($LASTEXITCODE) { throw '编译检查未通过' }
    Write-Host "[OK] 编译检查通过" -ForegroundColor Green
    exit 0
}

$ldf = @()
if ($Mode -eq 'Release') { $ldf += @('-H', 'windowsgui', '-s', '-w') }
if ($Version) { $ldf += @('-X', "main.version=$Version") }
$ldstr = $ldf -join ' '

Write-Host "==> go build ($Mode) -> $Out" -ForegroundColor Cyan
go build -trimpath -ldflags "$ldstr" -o $Out .
if ($LASTEXITCODE) { throw '构建失败' }

$mb = [math]::Round((Get-Item $Out).Length / 1MB, 1)
Write-Host "[OK] 构建完成: $Out ($MB MB)" -ForegroundColor Green
