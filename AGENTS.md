# Godrome 仓库约定

Claude/Codex 共同遵守本文件。目标和边界见 `docs/intent.md`；本次最小范围见 `docs/spec.md`。

- 维持 Go + MyGO 原生 UI + 进程内 libmpv；不引入 Web UI、后台服务或数据库。
- 依赖版本由 `go.mod` / `go.sum` 固定。升级必须说明原因；不使用 `@latest` 构建。
- 网络、图片解码、libmpv 初始化和命令在后台执行。UI 状态通过 `Window.Update` 更新。
- 密码、认证 token、带认证的 URL 只保存在会话内存中，禁止写入源码、配置、日志或测试证据。
- TLS 正常验证；请求接受并遵守 context 取消。保持分页、图片缓存和任务队列上限。
- `make build check test` 是基本检查；播放器改动还运行 `make test-audio`。
- 每次改代码都同步更新对应文档，并和代码放在同一次提交里。可观察行为写入 `docs/spec.md`，目标与界面写入 `docs/intent.md`，验收与测量写入 `docs/acceptance.md`。不要只改代码，也不要把未记录的改动留在工作区。
- `go tool mygo build` 在固定版本中默认禁用 cgo；播放构建用本项目的 `make build`，或显式 `CGO_ENABLED=1 go build`。
- 测试中的无声输出、UI tester、跨平台无 cgo 构建和真实桌面操作必须分开报告。
- 不主动 GC 美化内存数据，不把短时基线写成长期稳定性验收。不公开发布或自动上传。
