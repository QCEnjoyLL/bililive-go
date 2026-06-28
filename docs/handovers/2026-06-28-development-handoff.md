# 开发历程交接文档（截至 2026-06-28）

本文用于交接当前项目的开发背景、已完成改动、验证方式、发布习惯和后续风险点。当前仓库最新已发布版本为 `v1.2.1`，本地还有一组登录页图标与更新后自动跳转登录页的修复尚未发布。

## 当前状态

- 当前分支：`master`
- 当前远端状态：`origin/master` 指向 `v1.2.1`
- 最新已发布版本：`v1.2.1`
- 建议下一个补丁版本：`v1.2.2`
- 本地服务：已用 `go run build.go dev` 重新构建并启动 `bin/bililive-windows-amd64.exe`
- 当前未提交改动：
  - `src/servers/auth.go`
  - `src/servers/server.go`
  - `src/servers/handler_test.go`
  - `src/webapp/src/component/update-page/index.tsx`

这些未提交改动修复了两个问题：

1. 登录页图标裂开：`/favicon.ico` 与 `/manifest.json` 现在会被 WebUI 鉴权中间件放行，`/favicon.ico` 会按实际 PNG 内容返回 `Content-Type: image/png`。
2. 更新后等待页卡住：更新页轮询 `/api/info` 时，如果服务恢复但会话过期返回 `401/403`，会自动跳转到 `/login?next=当前页面`，重新登录后继续回到更新相关页面。

## 用户约定

- 小修复使用补丁版本号加一，例如 `v1.2.1` 到 `v1.2.2`。
- 较大改动使用中版本号加一，例如 `v1.1.x` 到 `v1.2.0`。
- 重大改动或新功能使用大版本号加一。
- 优先保持本地 `bin/bililive-windows-amd64.exe` 为最新测试构建，用户本地确认没问题后再发布。
- 本地测试版本使用 `-dev` 语义；正式发布去掉 `-dev`。
- 每次发布都需要写中文更新说明，并让发布 workflow 同步使用拟定好的更新说明。
- 仓库默认只保留 `config.example.yml`，不要再提交真实 `config.yml`。
- 不要提交 `.appdata/`、`bin/`、本地日志、真实配置、前端 `node_modules/`、`package-lock.json` 等本地生成物。
- 前端构建后如果产生 `src/webapp/node_modules` 或 `src/webapp/package-lock.json`，发布前清理掉。

## 开发主线

### 1. BoyFriend / MOUFLON HLS 录制稳定性

最早的核心目标是解决 BoyFriend 站点 HLS 录制卡顿、跳秒、丢段问题。实现路径经历了多轮：

- 将 `hlsmouflon` 从串行轮询下载改为高频轮询、分段调度、并发下载、按 `msn/part` 顺序写入。
- playlist 轮询间隔从约 `1500ms` 降低到更积极的轮询策略，降低滑动窗口跳过概率。
- 不再在分段下载前立即标记 `seen`，而是在成功写入或明确跳过后标记，避免临时网络失败造成永久漏段。
- 为 MOUFLON 分段解析稳定排序键，包括 `msn`、`part` 和真实 URL。
- 增加诊断日志：累计写入、新发现、确认丢段、疑似漏看、下载失败、重试成功、队列、写入等待、`liveLag`、playlist 窗口、最大单段下载耗时等。
- 增加临时监视日志，输出覆盖率和风险判断，例如 playlist 滑窗漏段、窗口过短、定向 playlist 回退、单段下载慢。
- 增加定向 playlist 探测 `_HLS_msn`，并根据 403、404、连续未命中等情况回退或继续探测补洞。
- 修复 MOUFLON 分段 hash 解析过窄的问题，支持带有 `+`、`-`、`_` 等 base64 风格字符的分段 hash。
- 增加 `scripts/analyze-media-gaps.ps1`，用 `ffprobe` 辅助分析录制文件中的音视频时间戳缺口。

当前判断：

- `v1.1.15` 以后，日志里的“确认丢段”已明显减少，部分测试可达到确认丢段 0、疑似漏看 0、覆盖率 100%。
- 用户仍观察到“画面短暂停住、音频连续”的卡顿。这类情况更像源流视频轨本身短时不出新帧，或视频编码/播放器表现问题，而不一定是 HLS 分段漏录。
- 仍建议继续用诊断日志和 `analyze-media-gaps.ps1` 区分“录制漏段”和“源流视频轨停顿”。

### 2. 更新系统与发布说明

更新系统经历了几轮修复：

- 修复 GitHub API 403 限流时更新检查失败的问题，加入更清楚的错误展示和 fallback 思路。
- 更新页面补充中文说明，发布日期改为北京时间展示。
- 发布 workflow 调整为读取并同步中文 release notes。
- 每次发布都在 `docs/releases/` 写入对应版本说明。
- 解释并保留“优雅更新”和“强制更新”两种方式：
  - 优雅更新：等待正在录制的任务完成后切换版本。
  - 强制更新：立即切换，可能中断录制。
- 当前本地未发布修复：更新完成后如果新版本要求重新登录，更新等待页会自动跳登录页，不再无限等待。

### 3. Docker 与辅助组件准备状态

Docker 版本曾出现 WebUI 启动慢、FFmpeg 尚未下载就开始录制的问题。已做过的方向：

- Web 服务不再被 FFmpeg 或外部工具下载长时间阻塞。
- 录制前会等待关键工具准备，避免 `ffmpeg executable file not found in PATH`。
- WebUI 顶部增加辅助组件准备状态提示。
- 当录制环境可用后，提示进入 5 秒倒计时并自动隐藏。
- 顶部提示条做过收窄，并在 `Bililive-go` 前增加站点图标。

仍需留意：

- Docker 首次启动仍可能需要下载多个外部工具，日志会显示下载进度。
- 如果用户直接添加房间并开始录制，应确保 FFmpeg 已 ready；WebUI 后续可继续增强“可录制状态”的明确提示。

### 4. WebUI 安全、登录与会话

WebUI 曾经默认公开访问，已引入鉴权：

- 配置项位于 `auth`：
  - `enable`
  - `username`
  - `password`
- 登录页从浏览器 Basic Auth 弹窗改成独立页面。
- 增加退出登录按钮与功能。
- 登录页支持主题和配色。
- 当前本地未发布修复：登录页 favicon 与 manifest 已放行，避免鉴权开启后图标资源被拦截。

注意：

- 如果用户已经登录过，浏览器可能保留 session cookie，因此打开页面不一定马上出现登录页。
- 测试登录状态时可清理 cookie 或使用隐私窗口。

### 5. 主题、配色与页面一致性

WebUI 主题做过较多细节迭代：

- 引入接近 Codex 风格的浅色、深色、跟随系统主题。
- 增加多套配色，并按字母顺序展示。
- 下拉框改成更柔和的毛玻璃效果。
- 深色主题避免纯黑背景，减少黑字被覆盖的问题。
- 登录页、主页面、设置页、Tools 页面、Scheduler 页面尽量共用同一套主题变量。
- iOS 风格开关启用后为绿色标识，按下时有更自然的滑块宽度变化。
- 自动刷新开启时，循环图标会持续旋转。
- 主页面右上角增加手动刷新与自动刷新。
- 长页面增加回到顶部浮标。
- 配置页增加底部浮动保存按钮，保存后尽量保持当前滚动位置，不再跳回顶部。

仍需留意：

- Tools 和 Scheduler 是外部页面或外部工具管理界面，深色主题靠桥接 CSS 覆盖，后续改 UI 时容易出现局部白底、黑字、过亮或过暗的问题。
- Ant Design 与外部页面 class 混用时，深色主题回归测试很重要。

### 6. Tools / Scheduler / OpenList

外部工具页面做过较多修复：

- 修复 `/tools/` 页面加载失败。
- 修复 `/tools/api/...` 与根路径 `/api/...` 的请求路径适配。
- 多版本工具从“每个版本一张卡片”改成下拉选择版本安装。
- 修复展开详情时先闪过旧版多卡片布局的问题。
- 保留默认版本选择能力。
- 深色主题下增强“未安装”“已安装”“已禁用”“默认版本”“版本号”“运行平台”等信息可读性。
- OpenList 支持外部实例和内部安装实例：
  - 填写外部 OpenList 地址和 token 时优先使用外部实例。
  - 未配置外部实例时可使用 RemoteTools 准备的内部 OpenList。
- 增加 OpenList 配置校验能力，用于验证地址、token、存储位置是否可用。
- 云盘上传阶段已从占位日志推进到实际调用 OpenList API 上传录制文件。

需要继续关注：

- OpenList 的“存储位置”文案已经替代“存储名称”。
- 默认上传路径模板已调整过，后续要避免不同房间文件混在一起。
- 如果自动上传无效果，优先检查 pipeline 阶段是否启用、OpenList 校验是否通过、任务日志是否有上传阶段执行。

### 7. 配置文件与仓库整理

已完成的整理方向：

- 仓库默认配置改为 `config.example.yml`。
- 远端不再保留真实 `config.yml`。
- 发布产物需要包含一份最新默认配置示例。
- 清理过项目中过大的本地生成物，避免仓库膨胀。
- `test` 与 `tests` 目录曾被确认用途：包含不同层面的测试资源或历史测试内容，清理前需要确认是否仍被脚本或 CI 引用。

注意：

- `config.yml` 是用户本地真实配置，不要覆盖或提交。
- `.appdata/`、`Videos/`、`bin/`、日志文件属于本地运行产物。

## 常用验证命令

后端与整体：

```powershell
go test ./src/...
go vet ./src/...
go run build.go dev
```

前端：

```powershell
cd src/webapp
npm install
npm run build
```

前端构建后清理：

```powershell
Remove-Item -Recurse -Force src/webapp/node_modules
Remove-Item -Force src/webapp/package-lock.json
```

HLS 录制专项：

```powershell
go test ./src/pkg/parser/hlsmouflon
.\scripts\analyze-media-gaps.ps1 -Path "录制文件路径.mkv"
```

本地 WebUI 快速检查：

```powershell
Invoke-WebRequest -Uri "http://127.0.0.1:8080/login" -UseBasicParsing
Invoke-WebRequest -Uri "http://127.0.0.1:8080/favicon.ico" -UseBasicParsing
```

## 发布流程建议

1. 确认工作区只包含本次要发布的改动。
2. 更新或新增 `docs/releases/vX.Y.Z.md`，中文详细说明。
3. 运行前端构建，确认构建产物可用于本地 WebUI。
4. 清理前端临时依赖目录和 lock 文件。
5. 运行 `go test ./src/...` 与 `go vet ./src/...`。
6. 运行 `go run build.go dev`，确保本地 `bin` 中测试版可启动。
7. 本地启动服务并做关键页面冒烟测试。
8. 提交代码。
9. 打 tag，例如 `git tag -a v1.2.2 -m "v1.2.2"`。
10. 推送 `master` 和 tag。
11. 等待 GitHub Actions 发布流程完成，确认 release notes、Windows/Linux 产物、Docker 镜像都生成成功。

## 下一步建议

短期建议先处理当前本地未发布修复：

- 补一份 `docs/releases/v1.2.2.md`。
- 发布 `v1.2.2`，说明登录页图标修复和更新后自动跳登录页。
- 发布后让用户验证：
  - 鉴权开启时登录页图标是否正常。
  - 程序更新后是否能自动刷新或自动跳登录页。

中期建议继续观察：

- HLS 录制卡顿是否仍表现为“视频停顿、音频连续”。
- 用 `analyze-media-gaps.ps1` 对比用户看到的跳秒时间点，确认是录制分段缺口、视频轨时间戳缺口，还是源流视频帧停顿。
- Tools、Scheduler、Config、FileList、Danmaku 等页面在深色主题下的局部样式回归。
- OpenList 自动上传链路的 pipeline 日志和失败提示。

## 关键文件索引

- HLS MOUFLON 录制：`src/pkg/parser/hlsmouflon/`
- 媒体缺口分析脚本：`scripts/analyze-media-gaps.ps1`
- WebUI 鉴权：`src/servers/auth.go`
- Web 服务路由：`src/servers/server.go`
- 更新管理后端：`src/pkg/update/`
- 更新页面：`src/webapp/src/component/update-page/`
- 主题与 WebUI：`src/webapp/src/`
- Tools 页面相关：`src/tools/` 与 WebUI 中的 tools 入口
- OpenList：`src/pkg/openlist/`、`src/pipeline/stages/`
- 默认配置示例：`config.example.yml`
- 发布说明：`docs/releases/`
