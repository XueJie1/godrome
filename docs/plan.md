# 实施状态

- [x] 从 MyGO v0.2.15 官方 native 模板建立项目，固定 Go 和依赖。
- [x] 小型 cgo/libmpv 动态加载适配层、延迟初始化和事件/关闭生命周期。
- [x] Subsonic API、单服务器登录、分页专辑、曲目、搜索取消。
- [x] 虚拟列表、封面缓存、队列和流播放控制。
- [x] 封面与只读元数据的有界磁盘缓存、LRU 淘汰、离线回退与存储设置。
- [x] 配置白名单、错误脱敏、默认 TLS 验证和路径前缀支持。
- [x] 登录成功后默认把密码写入系统密钥环，下次启动自动登录；退出登录删除。
- [x] 模拟 API/原生 UI/一万曲目列表/封面内存约束/race 测试。
- [x] 四种自生成格式的实际 libmpv 无声测试、HTTP 流、暂停/seek/EOF/快速切歌。
- [x] 临时 Navidrome 和用户测试服务器的在线登录到播放闭环。
- [x] Linux 默认音频输出信号验证、原生截图、退出、release 短时内存基线。
- [x] Windows amd64 / macOS arm64 的无 cgo UI 编译；加入三平台原生 cgo CI。
- [ ] 在 Windows/macOS 真正运行 CI/native cgo 构建和自动测试。
- [x] 全键盘登录、浏览、曲目/队列选择、暂停、进度调整和退出登录的 UI tester 回归测试。
- [x] Linux 真实桌面自动化键盘/鼠标输入，进度拖动、暂停恢复、曲尾自动下一曲和正常退出；用户确认真实服务器歌曲可听到、无杂音。
- [ ] Linux 实际桌面由用户使用物理键盘/鼠标完成全流程人工操作；自动化输入不等同于这一项。
- [x] 独立 headless Sway 原生窗口中，浏览大量专辑/滚动封面/四格式播放/暂停各 3×60 秒、切歌 3×30 分钟；含 CPU/OpenGL 诊断与完整 RSS/PSS/Go 堆曲线。全部无声输出，与真实桌面证据分开。
- [ ] Windows/macOS 桌面播放和内存数据。

统一入口：`make build check test test-audio measure acceptance-build`。补充验收与不干扰桌面的测量入口见 [acceptance.md](acceptance.md)，原始证据与限制见 `report.md`。
