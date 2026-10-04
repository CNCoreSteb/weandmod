# We&Mod 发版脚本(交互式):打 tag 并推送,触发 GitHub Actions 自动发布
# 用法:
#   .\release.ps1                 交互菜单(选版本 -> 确认 -> 打 tag -> push)
#   .\release.ps1 -Version 1.2.0  指定版本
#   .\release.ps1 -DryRun         只演算,不动 git
param(
    [ValidateSet('patch','minor','major')]
    [string]$Bump,
    [string]$Version,
    [switch]$DryRun
)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)

function Get-LatestTag {
    $t = git describe --tags --abbrev=0 2>$null
    if ($LASTEXITCODE) { return $null }
    return $t.Trim()
}

function Bump-Version([string]$base, [string]$kind) {
    $major = 0; $minor = 0; $patch = 0
    if ($base -match '^v?(\d+)\.(\d+)\.(\d+)') {
        $major = [int]$Matches[1]; $minor = [int]$Matches[2]; $patch = [int]$Matches[3]
    }
    switch ($kind) {
        'major' { $major++; $minor = 0; $patch = 0 }
        'minor' { $minor++; $patch = 0 }
        default { $patch++ }
    }
    "$major.$minor.$patch"
}

Write-Host "`n=== We&Mod 发版 ===" -ForegroundColor Cyan

# 工作区检查
$dirty = git status --porcelain
if ($dirty) {
    Write-Host '警告: 工作区有未提交改动,建议先提交再发版' -ForegroundColor Yellow
    git status --short
    if ((Read-Host '仍要继续? (y/N)') -ne 'y') { exit 0 }
}

$latest = Get-LatestTag
$base = if ($latest) { $latest } else { 'v0.0.0' }
Write-Host "当前最新 tag: $base`n"

if (-not $Version) {
    if (-not $Bump) {
        Write-Host (" [1] patch -> v" + (Bump-Version $base 'patch'))
        Write-Host (" [2] minor -> v" + (Bump-Version $base 'minor'))
        Write-Host (" [3] major -> v" + (Bump-Version $base 'major'))
        Write-Host ' [4] 自定义版本号'
        Write-Host ' [0] 取消'
        switch (Read-Host '选择') {
            '1' { $Bump = 'patch' }
            '2' { $Bump = 'minor' }
            '3' { $Bump = 'major' }
            '4' { }
            default { Write-Host '已取消'; exit 0 }
        }
    }
    if ($Bump) { $Version = Bump-Version $base $Bump }
}
if (-not $Version) {
    $Version = (Read-Host '输入版本号(如 1.2.0)').Trim() -replace '^v', ''
}
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "版本号格式不正确: $Version(应为 x.y.z)" }

$tag = "v$Version"

# tag 冲突检查
$exists = git tag -l $tag
if ($exists) { throw "tag $tag 已存在" }
$remote = git ls-remote --tags origin $tag 2>$null
if ($remote) { throw "远程已存在 tag $tag" }

Write-Host "`n将执行:" -ForegroundColor Cyan
Write-Host "  git tag -a $tag -m `"We&Mod $tag`""
Write-Host "  git push origin $tag"
Write-Host "  => GitHub Actions 自动编译 WeAndMod.exe 并创建 Release`n"

if ($DryRun) {
    Write-Host '[DryRun] 不执行实际操作' -ForegroundColor Yellow
    exit 0
}
if ((Read-Host '确认发布? (y/N)') -ne 'y') { Write-Host '已取消'; exit 0 }

git tag -a $tag -m "We&Mod $tag"
if ($LASTEXITCODE) { throw '打 tag 失败' }
git push origin $tag
if ($LASTEXITCODE) { throw '推送失败,可手动: git push origin ' + $tag }

Write-Host "[OK] 已推送 $tag,等待 CI 完成发布" -ForegroundColor Green
Write-Host '  进度: https://github.com/CNCoreSteb/weandmod/actions'
