# Godrome

一个轻量的桌面音乐播放器，连接你自己的 Navidrome / Subsonic 音乐库。

原生中文界面，单进程运行：不需要浏览器，也不在后台启动服务。

## 能做什么

- 浏览专辑、播放整张专辑、搜索歌曲
- 支持 MP3 / FLAC / AAC / Opus
- 播放队列：播放、加入队列、上一首/下一首、循环
- 标记喜欢的歌曲、专辑和歌手
- 进度拖动、音量、一键静音
- 浅色 / 深色 / 跟随系统，可换主题色
- 十段均衡器和预设
- 封面和元数据可离线浏览（音频不缓存）
- Linux 上支持系统媒体控制（MPRIS）
- 直接播放本地音频文件

暂不支持：歌单编辑（目前只有入口）、歌词、托盘、随机播放、离线下载音频。

## 快速开始

### 1. 安装系统依赖

需要 **Go 1.27.1**、**C 编译器**、**GTK 3**（Linux）和 **libmpv 2**。

Linux 已完成实机验证；Windows 和 macOS 可以构建，但原生桌面验收尚未完成。

| 系统 | 安装命令 |
| --- | --- |
| Arch Linux | `sudo pacman -S go gcc gtk3 mpv` |
| Debian / Ubuntu | 安装 Go 1.27.1，然后 `sudo apt install build-essential libgtk-3-0 libmpv2`（较新发行版 GTK 包名为 `libgtk-3-0t64`） |
| macOS | 安装 Go 1.27.1 和 Xcode Command Line Tools，然后 `brew install mpv` |
| Windows | 安装 Go 1.27.1 与 MinGW-w64 GCC，把 `mpv-2.dll`（或 `libmpv-2.dll`）及其依赖放到 exe 同目录 |

### 2. 构建并启动

```sh
make build
./build/godrome
```

Windows（PowerShell）：

```powershell
$env:CGO_ENABLED = '1'
go build -buildvcs=false -trimpath -ldflags="-s -w -X github.com/egoist/mygo.production=1" -o build/godrome.exe .
```

### 3. 登录

在登录页填写：

| 项目 | 说明 |
| --- | --- |
| 服务器地址 | 完整地址，例如 `https://music.example.com`；有路径前缀也可写 `https://example.com/navidrome` |
| 用户名 / 密码 | 你的 Subsonic 账号 |

默认勾选「记住密码」。登录成功后密码保存在系统密钥环，下次启动自动登录。退出登录在 **设置 → 账号**，会同时删除已保存的密码。

## 快捷键

| 操作 | 按键 |
| --- | --- |
| 播放 / 暂停 / 继续 | `空格`（窗口聚焦时） |
| 打开选中项 / 播放 | `Enter` |
| 移动选择 | 方向键 |

焦点在输入框、按钮等控件上时，空格留给该控件。

## 日常使用

- **侧栏**：曲库、专辑、喜爱、歌单、设置。窗口变窄时自动收成图标栏。
- **顶部搜索**：输入停止 300 ms 后搜索，最多返回 100 首；清空搜索回到原页面。
- **右侧队列**：窗口窄于 860 时会折叠，变宽后自动恢复。
- **设置**：外观、声音、账号、存储四个标签。主题、均衡器和缓存设置会保存到下次启动。
- **离线浏览**：服务器连不上但之前缓存过内容时，页面照常显示，顶栏出现「离线缓存」图标。可在 **设置 → 存储** 关闭该行为、调整缓存上限或清除缓存。

## 播放本地音频

```sh
./build/godrome song.mp3 album.flac
./build/godrome --autoplay song.flac
```

运行选项要放在文件路径前面。

## 配置文件与缓存位置

| 内容 | 位置 |
| --- | --- |
| 设置 | 系统配置目录下的 `godrome/settings.json`（Linux：`~/.config/godrome/`） |
| 封面与元数据缓存 | 系统缓存目录下的 `godrome/cache`（Linux：`~/.cache/godrome/cache`），默认上限 256 MiB |
| 密码 | 仅保存在系统密钥环，不写入 JSON |

缓存可以随时在设置页清除；清除后封面会重新下载。

## 常见问题

**第一次播放提示缺少 libmpv**

安装系统 libmpv 后重启应用。缺少 libmpv 时仍能登录和浏览，只是不能播放。Go 的 `CGO_ENABLED=0` 构建同样无法播放。

**登录失败**

- 确认地址是完整 URL，包含 `http://` 或 `https://`
- 确认账户可以在浏览器或官方客户端登录
- 如果服务在反向代理后面，加上路径前缀

**断网后打不开曲库**

只有之前浏览过的专辑和歌曲能离线显示。音频不缓存，离线时不能播放未缓存过的歌曲。

**想从源码运行，不做构建**

```sh
make run
```

## 开发与验证

```sh
make check        # 格式检查 + go vet
make test         # 单元测试与 UI 测试
make test-audio   # libmpv 播放测试（需要 ffmpeg 生成样本）
make test-mpris   # Linux MPRIS 测试
```

- 代码结构与各文件职责：[README-dev.md](README-dev.md)
- 仓库约定：[AGENTS.md](AGENTS.md)
- 行为规格：[docs/spec.md](docs/spec.md)
- 功能边界：[docs/scope.md](docs/scope.md)
- 验收与测量：[docs/acceptance.md](docs/acceptance.md)、[docs/report.md](docs/report.md)

## 许可证

本项目使用 MIT 许可证，见 [LICENSE](LICENSE)。libmpv 由系统提供，遵循其自身的许可证。
