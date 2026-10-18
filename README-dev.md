# Godrome 开发导览

给改代码的人看：目录里有什么、每个文件干什么、先读哪里。

一句话：**Go + MyGO 原生中文界面 + 进程内 libmpv**，连接 Navidrome/Subsonic 播放音乐。单进程，无 Web UI、无数据库、不启动 mpv 子进程。

---

## 1. 目录总览

```text
godrome/
├── main.go                 程序入口：命令行参数、窗口、配置保存
├── app.go                  全部界面与页面状态（最大的文件）
├── nav.go                  后退/前进历史
├── playback.go             队列与循环模式的规则
├── repeat.go               循环模式枚举
├── equalizer.go            十段均衡器与预设
├── icons.go                SVG 图标
├── media.go                空格播放控制、MPRIS 快照与封面缓存
├── metrics.go              可选的 Go 堆测量 CSV
├── acceptance_main.go      独立验收驱动（不进入正式构建）
├── *_test.go               根包测试
├── Makefile                构建/检查/测试/测量入口
├── mygo.json               MyGO 打包配置
├── go.mod / go.sum         固定依赖
├── internal/               业务与底层服务
│   ├── config/             配置白名单 + 系统密钥环
│   ├── subsonic/           Subsonic/OpenSubsonic HTTP 客户端
│   ├── player/             libmpv 引擎与 cgo 桥
│   ├── cover/              封面下载、解码、LRU 缓存
│   ├── queue/              纯队列数据结构
│   └── mpris/              Linux 会话总线媒体接口（非 Linux 为空实现）
├── docs/                   规格、计划、验收、证据
├── scripts/                测量、验收、样本、临时 Navidrome 脚本
├── resources/icon.png      应用图标
├── testdata/               自生成音频样本（git 忽略）
├── build/                  构建输出（git 忽略）
└── .github/workflows/      CI（三平台 vet/test/build）
```

依赖方向（目标）：`main → app → playback/nav → internal/*`。详见根目录 `ARCHITECTURE.md`。

---

## 2. 根目录文件

| 文件 | 作用 |
| --- | --- |
| `main.go` | 解析 flag，加载 `internal/config`，创建 `player.Engine`，开窗口，绑定关闭时保存窗口尺寸/音量/栏宽，退出时销毁引擎。 |
| `app.go` | 主线程状态与所有视图：登录、曲库、专辑、搜索、喜爱、歌单占位、设置、队列、底栏。网络请求、选页、搜索防抖、喜欢、主题、均衡器界面都在这里。 |
| `nav.go` | `place`（一次页面访问）与 `history`（浏览器式后退/前进）。只记页面参数和歌曲 ID，不存歌曲数组或封面。 |
| `playback.go` | `playbackState`：队列 + 循环模式。上一首/下一首/EOF 后是否续播的规则只在这里。 |
| `repeat.go` | `repeatOff / repeatAll / repeatOne` 三态与切换顺序。 |
| `equalizer.go` | 10 段频率、增益范围、7 个预设、生成 libmpv `lavfi` 滤镜字符串。 |
| `icons.go` | 播放、暂停、队列、心形、音量、主题等内联 SVG。 |
| `media.go` | 把主线程播放状态转成 MPRIS 快照；桌面组件命令回到主线程；把当前封面写入用户缓存并以 `file://` 提供；`setVolume`、`seekTo`、`stopPlayback` 的统一入口。 |
| `media_test.go` | 空格暂停/恢复、文本输入不触发播放、MPRIS 快照映射与封面文件写入。 |
| `metrics.go` | `--metrics file.csv` 时每秒记录 Go 堆和 goroutine；不强制 GC。正式与验收程序共用。 |
| `acceptance_main.go` | `//go:build ignore` 的独立验收程序；`make acceptance-build` 才编译。自带模拟 Subsonic 服务与场景脚本（empty/albums/library/covers/四种格式/切歌/live）。 |
| `app_test.go` | 主要 UI 测试：用 MyGO UI tester + 模拟 Subsonic 覆盖登录、搜索、喜欢、历史、栏宽、队列、窄窗口、键盘操作等。 |
| `nav_test.go` | 历史记录单元测试（前进清空、同页不重复、搜索改字、恢复位置）。 |
| `equalizer_test.go` | 均衡器滤镜字符串与预设曲线测试。 |
| `online_integration_test.go` | 真实 Navidrome 只读测试；需 `GODROME_TEST_*` 环境变量，平时跳过。 |
| `AGENTS.md` | 仓库硬约定：技术栈、安全、必须同步文档、检查命令。 |
| `CLAUDE.md` | 只指向 `AGENTS.md` 与 `docs/spec.md`。 |
| `ARCHITECTURE.md` | 目标依赖方向、状态归属表、禁止事项、重构顺序。 |
| `README.md` | 用户向：安装依赖、运行、验证、测量。 |
| `Makefile` | `run / build / check / test / samples / test-audio / measure / acceptance-build`。 |
| `mygo.json` | MyGO 应用名、标识、版本、输出目录。 |

### `app.go` 内部导航（约 2300 行，按顺序）

| 行号（约） | 内容 |
| --- | --- |
| 1–110 | import、`playback` 接口、`screen` 枚举及其 `String/nav/legacyPage`。 |
| 111–215 | `app` 结构体全部字段、`newApp`、密钥环自动登录初始化。 |
| 216–340 | `dispatch`（回主线程）、播放事件监听、请求代次 `beginPage/finish`、历史位置恢复。 |
| 341–469 | 登录、`place` 生成、记录/恢复页面、`goTo/back/forward`。 |
| 470–556 | 曲库与曲目加载、喜爱页加载、会话内喜欢集合。 |
| 557–723 | 歌曲/专辑/艺人 `star`/`unstar` 写入与失败回滚。 |
| 724–886 | 专辑分页、打开专辑、搜索防抖与取消、清空搜索。 |
| 887–983 | 播放当前曲、播放整页、`control` 错误入 UI、保存密码、退出登录、本地文件、关闭。 |
| 984–1124 | 播放状态中文文案、时间格式、主题色/预设色、外观与均衡器配置转换。 |
| 1125–1193 | 应用主题、栏宽常量、封面/提示过渡动画、列表滚动条留白。 |
| 1194–1347 | 根 `view`、三栏布局决策、宽度换算与拖拽夹取、主栏、队列栏。 |
| 1348–1380 | 侧栏导航。 |
| 1381–1622 | 设置页：外观、主题色、均衡器、账号、持久化。 |
| 1623–1847 | 历史按钮、喜爱页三个标签、占位页、顶栏、提示条、登录页、封面、心形按钮。 |
| 1848–2010 | 专辑页、歌曲页、各页页眉。 |
| 2011–2120 | 虚拟歌曲列表（含行内喜欢/更多菜单）。 |
| 2121–2322 | 队列视图、图标按钮、静音与音量、窄窗队列浮层、底栏。 |

---

## 3. `internal/` 包

### `internal/config` — 配置与密码

| 文件 | 作用 |
| --- | --- |
| `config.go` | `Settings` 白名单（服务器、用户名、音量、窗口尺寸、栏宽、主题色、外观、均衡器）。`Load/Save` 带范围校验和原子写入。密码不在这里。 |
| `secret.go` | 用 `zalando/go-keyring` 把密码放入系统密钥环；保存/读取/删除，不写 JSON。 |
| `config_test.go` | 配置往返、非法值回退、密码密钥环往返（测试使用模拟密钥环）。 |

### `internal/subsonic` — 协议客户端

| 文件 | 作用 |
| --- | --- |
| `client.go` | `Song/Album/Artist/Starred` 类型；登录 ping、专辑分页、专辑详情、搜索、歌曲分页、喜爱读取、star/unstar、流地址、封面下载。负责 MD5 加盐 token、15 秒超时、4 MiB JSON 上限、1 MiB 封面上限、TLS 验证、拒绝跨源重定向、错误脱敏。 |
| `client_test.go` | 模拟服务器验证协议、分页、错误脱敏、不安全 URL 与重定向拒绝。 |

### `internal/player` — libmpv 引擎

| 文件 | 作用 |
| --- | --- |
| `player.go` | `Engine`：UI 只发命令，单个 worker goroutine 独占 libmpv；管理状态、进度、音量、可见性、播放代次、EOF/错误事件。 |
| `native.go` | `//go:build cgo`：调用 C 桥，设置 libmpv 选项（无视频、无配置、缓存 4 MiB、预读 5 秒等），监听属性，转换事件。 |
| `nocgo.go` | `//go:build !cgo`：同接口空实现，仅用于跨平台编译检查，不能播放。 |
| `bridge.c` | 运行时 `dlopen` libmpv 2（Linux/macOS/Windows），加载函数指针，创建实例、设置选项、观察属性、命令、等待事件、销毁。 |
| `bridge.h` | C 桥对 Go 暴露的 10 个函数声明。 |
| `mpv_client.h` | 内置的最小 libmpv 客户端 API 头文件（ISC 许可）。 |
| `state.go` | `displayState`：缓冲/seek 优先于暂停的显示状态规则。 |
| `locale.go` | cgo 下把 `LC_NUMERIC` 设为 C；避免中文桌面 locale 让 `mpv_create` 失败。 |
| `state_test.go` | 显示状态优先级测试。 |
| `locale_test.go` | 非 C 数字 locale 下仍能初始化 libmpv（无 GUI 测试）。 |
| `player_integration_test.go` | cgo 集成测试：四种格式、暂停恢复、seek、EOF、HTTP 流；无声输出单列。 |

### `internal/cover` — 封面缓存

| 文件 | 作用 |
| --- | --- |
| `cache.go` | 每页一个 `Cache`；2 个下载/解码 worker、32 个待处理槽、256×256 位图、8 MiB LRU；离开页面取消并释放。限制解码前图片尺寸。 |
| `cache_test.go` | worker 上限、缓存命中、取消与容量测试。 |

### `internal/queue` — 队列

| 文件 | 作用 |
| --- | --- |
| `queue.go` | 纯数据：替换、追加、选择、上/下一首、当前曲目；越界返回 false。 |
| `queue_test.go` | 边界与复制所有权测试。 |

### `internal/mpris` — Linux 桌面媒体接口

| 文件 | 作用 |
| --- | --- |
| `types.go` | `Snapshot`（曲目、时长、进度、状态、循环、音量、能力）与 `Controls`（播放、暂停、上下曲、seek、音量、循环、raise、quit）回调。 |
| `server_linux.go` | 在会话总线申请 `org.mpris.MediaPlayer2.godrome`，导出根接口、Player 接口和 `org.freedesktop.DBus.Properties`；发送 `PropertiesChanged` 与 `Seeked`；进度按播放状态外推，窗口隐藏时仍前进。 |
| `server_other.go` | 非 Linux 的空实现，保持跨平台编译。 |
| `server_linux_test.go` | 元数据、属性变化、进度外推单元测试；设置 `GODROME_MPRIS_TEST=1` 并在 `dbus-run-session` 下运行时，用真实总线验证属性、方法、信号。 |

---

## 4. `docs/` 文档

| 文件 | 内容 |
| --- | --- |
| `intent.md` | 产品目标、功能边界、长期约束。 |
| `spec.md` | **当前可观察行为**与技术约束；改行为必须改这里。 |
| `scope.md` | 本阶段包含/不包含的功能汇总。 |
| `plan.md` | 实施清单与完成状态。 |
| `report.md` | 最小原型验证报告与最初内存基线。 |
| `acceptance.md` | 补充验收记录（UI tester、无声输出、桌面测量分开写）。 |
| `dependencies.md` | 固定依赖版本与许可证。 |
| `frontend-improvements.md` | 前端审查与 P1/P2/P3 建议。 |
| `evidence/` | 截图、CSV、验收 JSON、图表；测量输出，不是源码。 |

---

## 5. `scripts/`

| 文件 | 作用 |
| --- | --- |
| `audio-samples.sh` | 用 ffmpeg 生成 mp3/flac/m4a/opus 正弦样本。 |
| `audio-signal.py` | 录制默认输出的 440 Hz 信号并统计，不保存音频。 |
| `measure.py` | 启动 release 二进制，采样 RSS/PSS/CPU/线程与可选 Go 堆。 |
| `acceptance.py` | 每个场景顺序跑三次独立原生窗口测量并汇总。 |
| `headless-acceptance.py` | 在私有 headless Sway 中跑验收，不碰当前桌面。 |
| `desktop-input.py` | 在 Sway 上只操作验收窗口（快照/按键/点击/拖拽/关闭）。 |
| `navidrome-test.py` | 临时目录建样本库和随机账号，启动本机 Navidrome 跑在线闭环，结束销毁。 |
| `real-server-test.py` | 对真实服务器做只读测试；终端无回显输入密码。 |
| `plot-acceptance.py` | 用已保存的 CSV/JSON 画内存与趋势图。 |

---

## 6. 其他目录

| 路径 | 作用 |
| --- | --- |
| `resources/icon.png` | 窗口/应用图标。 |
| `testdata/audio/` | `make samples` 生成的四种音频样本（git 忽略）。 |
| `testdata/acceptance-audio/` | 长样本，供 headless 验收（git 忽略）。 |
| `build/` | `build/godrome` 与 `build/godrome-acceptance`（git 忽略）。 |
| `.github/workflows/check.yml` | Ubuntu/Windows/macOS：`go vet`、`go test -race`、release build；Linux 另跑 libmpv 音频测试。 |

---

## 7. 建议阅读顺序

1. `main.go` —— 程序怎么启动、窗口怎么绑定。
2. `playback.go` + `repeat.go` —— 播放规则（很短）。
3. `app.go` 的 `screen`、`app` 结构体、`view` —— 页面状态与三栏布局。
4. `internal/subsonic/client.go` —— 数据从哪来。
5. `internal/player/player.go` —— 声音怎么出。
6. `ARCHITECTURE.md` —— 哪些规则不能破坏。

## 8. 常用命令

```sh
make build      # 正式构建 -> build/godrome
make check      # gofmt 检查 + go vet
make test       # go test -race ./...
make test-audio # 生成样本并跑 libmpv 无声集成测试
make run        # 直接运行
```

约定：改行为要同步改 `docs/spec.md`、相关文档和代码，放在同一次提交；`AGENTS.md` 是最终约束。
