# Godrome 最小原型

Go + MyGO 原生中文界面，libmpv 在同一进程内播放 Navidrome/Subsonic 音频。

本阶段做什么、暂不做什么及验收边界见 [功能边界](docs/scope.md)。

已实现单服务器登录、专辑分页与曲目、歌曲搜索、喜爱、队列、自动下一曲、暂停恢复、上下曲、进度拖动和音量。没有离线下载、歌单编辑、歌词或托盘。

## 运行

需要 **Go 1.27.1、C 编译器、GTK 3（Linux）和 libmpv 2**。Go 依赖已固定。

```sh
make build
./build/godrome
```

登录界面填服务器完整地址、用户名和密码。默认记住密码：登录成功后写入系统密钥环，下次启动自动登录。退出登录在设置页，会删除已保存的密码。支持 `/music` 等路径前缀。配置默认存放在系统配置目录的 `godrome/settings.json`，保存地址、用户名、音量、窗口尺寸、侧栏宽度、队列宽度、主题色、外观和均衡器；密码不写入该文件。

```sh
make samples
./build/godrome testdata/audio/sample.mp3 testdata/audio/sample.flac
```

命令行可打开本地音频；加 `--autoplay` 自动开始，`--audio-output null` 使用测试无声输出。运行选项必须放在文件路径之前。

## 系统依赖

| 系统 | 安装方式 |
| --- | --- |
| Arch Linux | `sudo pacman -S go gcc gtk3 mpv ffmpeg` |
| Debian/Ubuntu | 安装 Go 1.27.1，然后 `sudo apt install build-essential libgtk-3-0 libmpv2 ffmpeg`（较新发行版 GTK 包名为 `libgtk-3-0t64`） |
| macOS | 安装 Go 1.27.1 和 Xcode Command Line Tools，然后 `brew install mpv ffmpeg`；加载器支持 Apple Silicon/Intel Homebrew 路径 |
| Windows | 安装 Go 1.27.1 与 MinGW-w64 GCC，获取与应用架构相同的 libmpv 2 共享库；将 `mpv-2.dll` 或 `libmpv-2.dll` 及其依赖放在 exe 同目录 |

Windows 构建（PowerShell）：

```powershell
$env:CGO_ENABLED = '1'
go build -buildvcs=false -trimpath -ldflags="-s -w -X github.com/egoist/mygo.production=1" -o build/godrome.exe .
```

缺少 libmpv 时仍能启动和浏览，首次播放会显示安装提示。`CGO_ENABLED=0` 构建用于检查 UI 跨平台编译，不能播放。MyGO v0.2.15 的打包 CLI 默认关闭 cgo，因此使用上述构建入口。

## 验证

```sh
make check test
make test-audio                    # FFmpeg 生成四种样本，libmpv 无声输出测试
python3 scripts/navidrome-test.py /path/to/navidrome
```

最后一条命令使用 Navidrome 0.64.2，在临时目录建立样本曲库和随机测试账号，监听 loopback，测试结束销毁实例；不依赖个人账号。脚本不自动下载安装服务器。

真实服务器测试是可选的，只读取专辑、搜索和流。通过环境变量 `GODROME_TEST_SERVER`、`GODROME_TEST_USER`、`GODROME_TEST_PASSWORD` 提供本次测试凭据，再运行 `go test -race -run '^TestRealNavidrome$' -v .`。不要把密码写进文件或报告。

截图使用模拟曲库，可复现：

```sh
GODROME_UI_EVIDENCE=docs/evidence/ui go test -run TestLoginAndOnlineViews .
```

## 测量与当前状态

```sh
make measure                      # 空窗口，每次 30 秒，共三次
python3 scripts/measure.py --seconds 30 --runs 3 --output docs/evidence/memory/playback -- ./build/godrome --autoplay /path/to/audio.flac
```

Sway 下加 `--sway-size` 固定被测窗口为 1080×720；其他桌面应手动固定尺寸和缩放。可用 `--metrics /tmp/heap.csv` 单独记录 Go 堆。测量不强制 GC。

本次交付是最小可运行实现。已完成与待完成的证据、内存基线和限制见 [原型报告](docs/report.md)，完整验证计划仍以 [intent.md](docs/intent.md) 为准。

补充验收与独立隐藏显示环境的测量方法见 [验收记录](docs/acceptance.md)。可用 `make acceptance-build` 构建单独的验收工具；`python3 scripts/headless-acceptance.py --output docs/evidence/acceptance-new` 顺序执行三轮 30 分钟切歌和各场景三次测量，全程无声，不操作当前桌面。运行前用 `GODROME_SAMPLE_DURATION=20 sh scripts/audio-samples.sh testdata/acceptance-audio` 生成样本。

真实服务器只读测试也可使用 `python3 scripts/real-server-test.py <服务器地址> <用户名>`，在终端无回显输入密码。`--native` 会打开有声原生窗口，测试音量上限 50%；仅在方便操作桌面时使用，不与隐藏测量混为同一验收。
