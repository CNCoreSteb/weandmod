# We&Mod

游戏修改器获取工具 (Go + Fyne)。

## 功能

- 首页搜索:实时过滤本地游戏库,并在线搜索 FLiNG 修改器
- 游戏库扫描:Steam(多库目录)、Epic、GOG,以及自定义目录(扫描 .exe)
- 一键为某个游戏搜索对应修改器,打开下载页

## 本地构建

需要 Go ≥1.27 和 MinGW gcc(Fyne 依赖 CGO)。Windows 下运行:

```bat
build.bat
```

产出单文件 `WeAndMod.exe`(GUI 子系统,无控制台窗口)。

## CI

`main` 分支 push / PR / `v*` tag 都会触发 GitHub Actions 自动编译 Windows amd64 单文件 exe:
- push/PR → 上传 Actions artifact(`WeAndMod-windows-amd64`)
- `v*` tag → 自动创建 GitHub Release 并附带 exe

## 设置数据

自定义扫描目录和上次扫描缓存保存在 `%APPDATA%\WeAndMod\`。
