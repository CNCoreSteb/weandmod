# We&Mod

游戏修改器获取工具(Go + Fyne),自动扫描本机游戏库并搜索对应修改器。

## 功能

- **首页搜索**:输入关键词实时过滤本地游戏库,同时在线搜索修改器站点(当前接入 FLiNG)
- **游戏库扫描**:Steam(含多库目录)、Epic、GOG 自动检测 + 自定义目录(扫描 .exe)
- **一键找修改器**:每个游戏行内"找修改器"按钮自动带入游戏名搜索
- **结果直达**:修改器结果"打开页面"跳转到对应下载页

## 构建与运行(Windows)

需要 Go ≥1.27 和 MinGW gcc(Fyne 依赖 CGO;可用 `winget install BrechtSanders.WinLibs.POSIX.UCRT` 安装)。

`scripts/` 下提供交互式脚本,双击 `.cmd` 或命令行运行 `.ps1` 均可:

| 脚本 | 作用 | 参数 |
| --- | --- | --- |
| `scripts\build.ps1` | 打包:Release(无控制台/去符号)/ Debug / Check | `-Mode Release [-Version v1.0.0] [-Out WeAndMod.exe]` |
| `scripts\run.ps1` | 调试运行:直接跑 / race 检测 / 先 vet+test 再跑 | `-Mode Run\|Race\|Test` |
| `scripts\release.ps1` | 发版:选版本号 → 打 tag → push 触发 CI 发布 | `-Bump patch\|minor\|major` / `-Version 1.2.0` / `-DryRun` |

不传参数时进入交互菜单;传参则非交互执行(供 CI 调用)。

也可以直接 `build.bat`(仓库根)打默认 Release 包。

## CI / 发布

`.github/workflows/build-windows.yml` 在 push、PR、`v*` tag 时自动编译 Windows amd64 单文件 exe(调用 `scripts/build.ps1`):

- **push / PR** → 上传 Actions artifact `WeAndMod-windows-amd64`
- **`v*` tag** → 自动创建 GitHub Release 并附带 `WeAndMod.exe`,版本号注入窗口标题

推荐用 `scripts\release.ps1` 发版:交互式选 patch/minor/major,确认后打 tag 并推送。

## 目录结构

```
main.go                     入口(版本号由 -ldflags 注入)
internal/game/              游戏模型
internal/scan/              游戏库扫描(steam/epic/gog/custom + 自写 VDF 解析)
internal/provider/          修改器提供方插件框架(注册表 + 能力接口 + 聚合)
internal/provider/fling/    FLiNG 适配器(搜索 + 详情页下载链接解析)
internal/store/             设置与缓存(%APPDATA%\WeAndMod)
internal/ui/home.go         首页 UI
scripts/                    交互式构建/运行/发版脚本
```

## 接入新的修改器站点

1. 新建 `internal/provider/<站点>/` 子包,实现 `provider.Provider`(`ID/Name/BaseURL`)
2. 按需实现能力接口:`Searcher`(搜索)/ `DownloadResolver`(直链解析)
3. `init()` 里 `provider.Register(...)`
4. `main.go` 加一行 blank import

UI 无需改动,`SearchAll`/`Resolve` 自动发现并聚合。

## 数据位置

自定义扫描目录与上次扫描缓存:`%APPDATA%\WeAndMod\`
