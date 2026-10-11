# 最小原型验证报告

日期：2026-10-07。交付范围见 `spec.md`；本文件保留最初的短时基线。后续 3×30 分钟切歌与完整场景矩阵已完成，见补充报告；跨平台原生验收仍未完成。

后续补充验收、播放器媒体标题脱敏修复及独立 headless 测量结果见 [acceptance.md](acceptance.md)。原始短时基线保留，不用新环境数据替换。

## 实现与构建

- 已有可运行 Linux release 二进制：`build/godrome`，约 13 MiB。
- MyGO v0.2.15 官方 native 模板；Go 业务包 + 小型 cgo/libmpv C API 适配层；无 Web UI、数据库或 mpv 子进程。
- 功能：登录、专辑分页、专辑歌曲、歌曲搜索、队列与自动续播、暂停恢复、上下曲、进度、音量、停止/重试。
- 封面后台缩略图、2 个下载/解码 worker、8 MiB 解码缓存；搜索取消与结果代次校验；配置和错误消息不含密码/token/认证 URL。
- `go vet ./...`、`go test -race ./...` 和 release 构建通过。Windows amd64/macOS arm64 无 cgo UI 构建通过；该二进制不含播放能力。三平台原生 cgo CI 已配置，本次未实际运行 Windows/macOS CI。

## 已执行的验证

| 验证 | 结果和边界 |
| --- | --- |
| 模拟 Subsonic API | 新随机盐、token、URL 前缀、分页参数、Unicode 搜索、取消、超时、HTTP/认证错误、跨源重定向和错误脱敏通过 |
| MyGO `ui.NewTester` | 登录→专辑→歌曲→队列、暂停、EOF 续播、错误保留队列、重试、退出登录；深色/150% 缩放；一万条歌曲虚拟列表可到最后一项 |
| 搜索 | 300 ms debounce、未发出的输入被取消、已发出的旧请求取消、旧结果/旧播放代次丢弃通过 |
| 封面 | 两个 worker 的并发上限、缓存命中、8 MiB 淘汰和 context 取消通过；下载/图片尺寸限制见实现 |
| 实际 libmpv | 自生成 MP3、FLAC、AAC/M4A、Opus；播放、暂停/恢复、seek、音量、正常 EOF、20 次快速替换、错误、重复关闭通过；使用 `ao=null`，其中 FLAC/Opus 经临时 HTTP 直接流播放 |
| 临时 Navidrome 0.64.2 | 随机临时账号和自生成曲库，`/music` 前缀；登录、专辑列表/详情、搜索、原始流、暂停/恢复、UI Slider seek、停止通过；无声输出，实例已退出和销毁 |
| 用户明确提供的测试服务器 | 同一在线 UI→API→libmpv 闭环通过；只读测试、无声输出；凭据仅进入测试进程环境/内存，报告和配置不保存密码 |
| Linux 原生窗口 | 真实桌面启动，默认系统输出播放 FLAC，正常退出，exit code 0。默认 sink monitor 录得样本音频信号；这不替代用户的主观听感验收 |

截图：[专辑](evidence/ui/albums.png)、[队列](evidence/ui/queue.png)、[深色 150%](evidence/ui/dark-150.png)来自 tester 的模拟曲库。[Linux 原生窗口](evidence/ui/native-linux.png)为实际 Sway 桌面截图，播放本地样本。[音频证据](evidence/native-output.json)包含退出码、输出信号统计和窗口尺寸，不含音频/用户内容。

## Linux 短时内存基线

环境：Arch Linux x86_64，Linux 7.2.8-zen1-2-zen；GTK 3.24.52，Sway 1.12，eDP-1 1920×1080，系统缩放 125%；可用 NVIDIA RTX 3050 Laptop/P104-100，驱动 580.178.04。具体 MyGO 默认渲染设备未单独探测，不把可用设备列表视为实际使用的 GPU。libmpv client 2.5.0 / mpv 0.41.0。使用 release、MyGO 默认渲染，不强制 GC。

每个场景三次，每次 10 秒，Sway 将该 PID 的窗口固定为 1080×720 逻辑像素。进程每 0.5 秒采样，Go 堆每秒采样。下表“稳定值”是第 5–10 秒的中位数，只是短时窗口，不是长期稳定性结论。FLAC 使用 20 秒自生成样本，所以采样期持续播放。

| 场景 | RSS 稳定值 MiB（三次） | PSS 稳定值 MiB（三次） | RSS 峰值 MiB（三次） | Go HeapAlloc MiB（三次） | 线程（三次） | CPU 稳定中位数 %（三次） |
| --- | --- | --- | --- | --- | --- | --- |
| 空窗口，未初始化 libmpv | 64.53 / 66.59 / 64.72 | 35.50 / 37.49 / 35.55 | 78.48 / 80.55 / 78.68 | 3.17 / 3.18 / 3.18 | 24 / 25 / 25 | 0 / 0 / 0 |
| 登录界面，未初始化 libmpv | 73.39 / 73.56 / 71.59 | 39.73 / 39.91 / 37.92 | 86.09 / 85.74 / 84.02 | 3.31 / 3.32 / 3.30 | 23 / 23 / 21 | 0.99 / 0 / 0 |
| 本地 FLAC，系统默认输出 | 161.93 / 159.98 / 159.88 | 99.06 / 96.84 / 96.85 | 161.94 / 160.00 / 159.89 | 3.47 / 3.58 / 3.47 | 36 / 38 / 36 | 1.99 / 2.98 / 1.99 |

原始数据：[CSV 目录](evidence/memory)、[汇总 JSON](evidence/memory/summary.json)。PSS 峰值、goroutine 数也在汇总中。本次窗口采样不是启动瞬时峰值的完整追踪；RSS/PSS 采样可能错过小于 0.5 秒的尖峰。CPU 百分比按一个核心计。

从登录到首次播放，Go 堆增加很少，进程 RSS/PSS 增加明显，主要成本更可能来自 libmpv 和其动态库/音频后端；这是阶段比较推断，没有做逐库归因。预算建议：下一阶段分别跟踪 Go 堆、渲染和 libmpv 原生开销，不据这三次短测制定硬性 100/150 MiB 上限。

复现：

```sh
make build
GODROME_SAMPLE_DURATION=20 sh scripts/audio-samples.sh /tmp/godrome-measure-audio
python3 scripts/measure.py --sway-size --seconds 10 --runs 3 --output docs/evidence/memory/empty -- ./build/godrome --empty
python3 scripts/measure.py --sway-size --seconds 10 --runs 3 --output docs/evidence/memory/login -- ./build/godrome
python3 scripts/measure.py --sway-size --seconds 10 --runs 3 --output docs/evidence/memory/flac -- ./build/godrome --autoplay /tmp/godrome-measure-audio/sample.flac
```

## 仍待完整验收

- 独立 headless 环境的大曲库、封面、四格式、暂停、3×30 分钟切歌、驱动加载记录与 CPU/OpenGL 对照已完成，详见补充报告；逐库/原生分配精确归因尚未完成。
- Linux 真实桌面自动化键盘/鼠标、滑动进度、暂停恢复、曲尾续播和用户主观听感已确认；物理键盘/鼠标人工全流程仍待验收。
- Windows/macOS 原生 cgo 构建和可执行自动测试、真实音频输出及内存数据。

本次证明了最小在线闭环可用，尚不宣称 `intent.md` 的完整原型或三端运行兼容验收完成。
