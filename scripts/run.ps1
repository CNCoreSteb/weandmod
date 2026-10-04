# We&Mod 调试运行脚本(交互式)
# 用法:
#   .\run.ps1           交互菜单
#   .\run.ps1 -Mode Run   go run .
#   .\run.ps1 -Mode Race  带 race 检测运行
#   .\run.ps1 -Mode Test  vet + test 通过后运行
param(
    [ValidateSet('Run','Race','Test')]
    [string]$Mode
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
    Write-Host "`n=== We&Mod 调试运行 ===" -ForegroundColor Cyan
    Write-Host ' [1] 直接运行       go run .'
    Write-Host ' [2] 竞态检测运行   go run -race .(慢,需 gcc)'
    Write-Host ' [3] 检查 + 运行    vet + test 后 go run .'
    Write-Host ' [0] 取消'
    switch (Read-Host '选择') {
        '1' { $Mode = 'Run' }
        '2' { $Mode = 'Race' }
        '3' { $Mode = 'Test' }
        default { Write-Host '已取消'; exit 0 }
    }
}

Find-Tool 'go'  'D:\DevTools\tools\*\go\bin\go.exe'
Find-Tool 'gcc' "$env:LOCALAPPDATA\Microsoft\WinGet\Packages\BrechtSanders.WinLibs*\mingw64\bin\gcc.exe"
$env:CGO_ENABLED = '1'

if ($Mode -eq 'Test') {
    Write-Host "==> go vet" -ForegroundColor Cyan
    go vet ./...
    if ($LASTEXITCODE) { throw 'go vet 未通过' }
    Write-Host "==> go test" -ForegroundColor Cyan
    go test ./...
    if ($LASTEXITCODE) { throw '测试未通过' }
}

# 不带 -H windowsgui,日志直接打到控制台
if ($Mode -eq 'Race') {
    go run -race .
} else {
    go run .
}
