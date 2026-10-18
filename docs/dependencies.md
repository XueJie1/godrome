# 固定依赖与许可证记录

记录日期：2026-10-07。

| 依赖 | 固定或实测版本 | 许可证/用途 |
| --- | --- | --- |
| Go | go.mod 1.27.1；本机 go1.27.1-X:nodwarf5 | BSD 风格；构建 |
| github.com/egoist/mygo | v0.2.15 | MIT；原生 UI/桌面窗口 |
| github.com/ebitengine/purego | v0.11.1 | Apache-2.0；MyGO 平台调用 |
| github.com/go-text/typesetting | v0.3.5 | BSD-3-Clause/Unlicense；字体排版 |
| golang.org/x/image | v0.46.0 | BSD-3-Clause；封面解码和缩放 |
| github.com/zalando/go-keyring | v0.2.8 | MIT；把密码交给系统密钥环。Linux 走 Secret Service，macOS 走登录钥匙串，Windows 走 Credential Manager |
| github.com/godbus/dbus/v5 | v5.2.2 | BSD-2-Clause；Linux 会话总线上的 MPRIS 服务 |
| Lucide repeat / repeat-1 / arrow-right / disc-3 / chevron-left / chevron-right / heart / sun / moon / monitor / volume-2 / volume-x / library-big / disc-album / list-music / settings | 2026-10-11 源文件；侧栏图标 2026-10-18 源文件 | ISC，版权 Lucide Icons and Contributors；播放模式、封面占位、后退和前进、喜欢、设置页外观、底栏音量，以及侧栏导航 |
| libmpv | pkg-config 2.5.0，mpv 0.41.0，Arch 包 1:0.41.0-6，ABI libmpv.so.2 | 系统包声明 GPL-2.0-or-later AND LGPL-2.1-or-later；进程内音频播放 |
| mpv client.h | 本机 2.5.0 头文件副本 `internal/player/mpv_client.h` | ISC，完整版权声明保留；C API ABI |
| FFmpeg | n9.0.1，运行库 n9.0.2 | 仅用于生成测试样本；不是额外打包依赖 |
| Navidrome | 0.64.2，仅临时测试 | GPL-3.0；独立测试服务器 |

Go 间接依赖以 `go.mod`/`go.sum` 的锁定内容为准，不在每次构建升级。系统 libmpv 不随应用下载、复制或打包。缺失时播放器显示错误。

MyGO 官方模板来源：[native 模板](https://github.com/egoist/mygo/tree/v0.2.15/cmd/mygo/template/native)。libmpv 核心默认 GPLv2+，禁用 GPL 功能的构建为 LGPLv2+；客户 API 头文件为 ISC，见 [mpv client API](https://github.com/mpv-player/mpv/blob/v0.41.0/include/mpv/client.h)。当前系统包不能据头文件许可证视作 ISC 核心。本次原型不发布分发包，正式分发前需确定应用许可与所使用的 libmpv 构建。

临时 Navidrome 测试使用 [0.64.2 官方 Linux amd64 归档](https://github.com/navidrome/navidrome/releases/tag/v0.64.2)，SHA-256：`fdd87fd107818667c2c50bc24dc6ce856962c795b3822a91f95c066cfad2c53e`。
