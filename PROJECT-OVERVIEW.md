# 指纹云控项目总览

更新日期：2026-10-04。本文件是后续开发的统一阅读入口，汇总目标、进度、目录关系、开发定位、构建和整理规则。详细协议与历史验证记录仍保留在原文档中；其中旧状态与本次现场核对不一致时，以本文件的日期和验证边界为准。

## 1. 项目目标与边界

基于指纹 Chromium 构建 PC / Android 多账号浏览器：一个父窗口、唯一且不可关闭的 SaaS 控制台、每个账号最多一个运行环境 Tab。账号通过稳定 `account_id` 绑定独立持久化 StoragePartition，隔离 Cookie、网页存储、缓存、Service Worker 和网络上下文，并具有账号级代理、UA / UA-CH、硬件并发数与指纹种子。

“指纹云控”业务独立部署：Web 管登录、工作区、账号目录、成员权限、设备、同步与审计；Go 服务管鉴权、权限、版本、租约与加密信封；Chromium 只提供受完整 Origin 白名单保护的本地能力。普通浏览器可以管理云端业务，原生能力不可用时必须明确显示状态，不生成假 Tab、假账号或假成功结果。

环境同步能力始终保留，用户选择具体数据类别。云端只保存客户端加密快照；环境加密密码与登录密码分离，不持久保存快照密码。IndexedDB、Cache Storage、Service Worker 的本地隔离不等于已经支持它们的跨设备快照同步。

本地基线由 `chromium_version.txt` 固定为 `144.0.7559.132`。根目录上游 README 的发布版本表不能代替本地源码版本，也不能作为本项目 SaaS 功能已发布的依据。

## 2. 当前进度

总体处于“主要功能源码及局部回归已完成，最新浏览器集成与发布验收待完成”阶段，不记录缺少验收依据的完成百分比。

| 目标模块 | 已有实现 | 当前验证边界与缺口 |
| --- | --- | --- |
| 单父窗口与账号环境 | 持久分区、账号代理/指纹、单账号 Tab、控制台关闭保护、幂等打开 | 旧 Development 有历史隔离测试；最新 SaaS 源码的完整运行回归未完成 |
| 独立 SaaS Web | 登录/邀请、工作区、账号搜索筛选与编辑删除、批量同步恢复、冲突、权限、设备、真实运行 Tab 条、移动布局 | 本次 36 项网页测试与全部业务 JS 语法检查通过；最新原生桥和真实移动端仍待联调 |
| Go 控制面 | 会话、角色、账号授权、目录、加密快照、条件版本、租约、审计、静态托管、限流、初始化用户 | 本次 `go test -v ./...`、`go vet ./...` 通过；独立 PostgreSQL 集成因未配置测试库跳过，历史通过记录保留 |
| 快照同步 | Cookie、LocalStorage、SessionStorage、指纹、代理、页面地址；PBKDF2 / AES-GCM；合并/覆盖版本校验 | 网页真实加密回归通过；原生与云端多设备运行闭环、其他存储类别未验收 |
| 桌面原生桥 | `tabs/storage/fingerprint/files/http/crypto`、精确来源校验、固定宿主通信 | 已有局部 C++ / TypeScript 验证记录；最新 Chrome 未链接，不能把源码能力视为当前二进制能力 |
| Android 桥 | 消息端口与 JNI、真实 TabModel、固定分区、存储、指纹、SimpleURLLoader、SAF、Keystore | 历史 JVM/协议累计 667 项通过；完整 Java/C++ 编译、APK/AAB、设备生命周期/权限/网络未验收，本次未复跑 |
| Worker 指纹 | Dedicated / 嵌套 / Shared / Service Worker 硬件快照传递，Service Worker UA / UA-CH | 已编码并保存补丁；`worker_fingerprint_verified=false`，实际请求头和运行一致性待验证 |
| 产品化与发布 | 商业界面首轮、头像入口隐藏补丁、单进程限流 | HTTPS 部署、多实例限流、配额/保留/计费、安装升级、完整发布验收待完成 |

### 构建现场核对

- `build/src/out/Development/build.ninja` 当前为 **6,903,783 字节**，已经包含实际构建图；“仅 558 字节、只剩重生成入口”属于旧现场记录。图文件存在不证明依赖、工具链和增量构建已全部恢复。
- Development 的 `chrome.exe` / `chrome.dll` 最近写入日期仍为 **2026-10-02**；Release 与 `publish/` 对应二进制为 **2026-09-30**。本次没有编译或重新链接 Chromium，不能声称最新补丁已经进入这些运行文件。
- 两套输出均保留 `args.gn`、`.ninja_deps`、`.ninja_log`、对象与生成文件；本次不执行 GN、Ninja 清理、源码重解包或系统环境变更。
- PC / Android 编译参数已在 `AGENTS.md` 设置强制锁：任何参数、工具链或输出/缓存路径变更，必须先给出旧值/新值、预计重编译源文件/编译单元数量及依据，取得用户针对本次变更的明确确认。现有参数与缓存不因设置规则而改变；规则不等于操作系统文件权限锁。
- 参数已纳入 Git：保留 `build-configs/` 模板和路径清单，另跟踪 PC Development / Release 的实际 `args.gn` 及 `build/src/build-configs/common.gn`；实际参数与模板的现有差异原样保存。其余构建产物与缓存不提交，Android 暂无实际输出参数文件。
- 旧记录中的 WSL 启动错误 `HCS_E_HYPERV_NOT_INSTALLED` 和组件入口加载失败本次没有复验，仅作为后续构建排障线索。已知 Visual Studio / Windows SDK 位于 D 盘，不能把旧失败笼统归因为 SDK 未安装。

## 3. 文件夹结构与职责

```text
chrome-finger/
├─ PROJECT-OVERVIEW.md          统一项目入口：目标、进度、结构、整理和开发规则
├─ AGENTS.md                   本项目任务边界；开发还须遵守用户提供的全局规则
├─ saas-web/                   独立 HTML/CSS/JS 业务前端与 .test.cjs 回归
├─ saas-server/                独立 Go 控制面
│  ├─ cmd/fingerprint-saas/     服务启动、受控 bootstrap-user
│  └─ internal/
│     ├─ auth/                 密码哈希、访问令牌、邀请摘要
│     ├─ config/               数据库、令牌、来源、限流配置
│     ├─ httpapi/              API、鉴权权限、静态托管、限流、集成测试
│     └─ store/                PostgreSQL、幂等迁移、首个用户初始化
├─ android-bridge/
│  ├─ src/main/                平台无关 Java/Kotlin 契约、加密与辅助模块
│  ├─ src/chromium/java/       消息宿主、SAF、Keystore 的真实平台实现
│  ├─ src/chromium/native/     账号环境/状态/存储/指纹/HTTP 原生实现
│  ├─ src/test/                独立 JVM 测试
│  └─ tests/                   JVM/网页互通、参数与脚本边界验证
├─ patches/                    可重放的 Chromium 修改，series 决定应用顺序
│  ├─ core/                    ungoogled / 基础行为补丁
│  ├─ extra/fingerprint/      基础指纹补丁
│  └─ upstream-fixes/          受管理 Tab、SaaS 桥、Android、Worker 等补丁
├─ build-configs/              PC/Android GN 参数与独立输出、缓存、工具链路径清单
├─ utils/                      下载、裁剪、域替换、补丁应用、局部 C++ 检查及 SaaS 打包
├─ devutils/                   补丁/配置校验与维护工具
├─ docs/                       进度、路线、契约、设计、历史测试及上游说明
│  └─ design/                 桌面/移动高保真 SVG/PNG；不是生产业务入口
├─ build/                      受保护的本地 Chromium 工作区及缓存，整目录保留
│  ├─ src/                    已展开并应用补丁的 Chromium 源码
│  │  └─ out/                 Development / Release 构建图、对象、生成文件和二进制
│  ├─ download_cache/         上游下载缓存
│  ├─ tool-cache/ npm-cache/  已有工具和依赖缓存
│  ├─ rust-toolchain*         工具链及已有恢复链接
│  └─ domsubcache.tar.gz      域替换缓存；其他测试配置、日志亦保留
├─ publish/                    已有 Chromium 运行发行目录和测试配置，整目录保留
└─ output/                     项目验证输出及独立 SaaS 归档；不作为 Chromium 编译目录
```

根目录的 `downloads.ini`、`pruning.list`、`domain_*.list`、`flags.gn`、版本文件与上游工具一起控制源码准备和基础配置；不是无用历史文件。`.github/` 保存上游 CI / 发布元数据流程，现有创建 Release 工作流不负责自动编译 SaaS 或 Chromium。

## 4. 模块关系与真实数据路径

```text
用户操作
  → saas-web/app.js / administration.js / operations.js
    → Go /api/v1 → 鉴权与权限 → PostgreSQL（目录、会话、授权、租约、密文、审计）
    → bridge-contract.js → 受信来源的 Chromium 桥 → 账号 StoragePartition / 平台能力

同步：账号本地状态 → snapshot-sync.js 客户端加密 → 服务端租约 + 条件版本写入 → 云端密文
恢复：云端密文 → 客户端解密及账号校验 → 空白账号环境 + 配置 → 写入所选存储 → 网站导航
```

- `account_id` 连接云端目录、快照与本地账号环境；`tab_id` 是本机运行标识，不能替代云端账号身份。
- `bridge-contract.js` 封装桌面固定 WebUI 宿主与 Android 消息端口，`.d.ts` 定义契约。普通页面不能自报来源获取权限。
- `session-persistence.js` 负责业务会话生命周期；有真实安全能力时持久保存刷新凭据/设备标识/服务地址，恢复仍须服务端验证。Android Keystore 层只保存不透明文本，不复制登录或工作区逻辑。
- JVM `SaasHttpClient` 是独立辅助模块，不是 Android Chromium HTTP 后端；Android 实际网络路径为原生 SimpleURLLoader 和账号分区。
- `android-bridge/src/chromium/` 与相关补丁保存平台实现；`build/src/` 是当前本地应用现场。修改本地 Chromium 时必须同步相应补丁，新增补丁登记 `patches/series`；不能只留在忽略目录里。

## 5. 后续修改定位

### 5.1 SaaS 与指纹 Chromium 的关系

**SaaS 决定账号业务和操作流程，指纹 Chromium 实际执行本机账号环境能力，两者通过版本化原生桥连接。** SaaS 是独立前端与 Go 服务，指纹 Chromium 是本地浏览器客户端；同一个仓库不代表它们共用业务职责或必须一起编译发布。

| 层级 | 应负责 | 不应承担 |
| --- | --- | --- |
| `saas-web/` 业务前端 | 页面、表单、登录交互、工作区/账号选择、同步编排、客户端快照加解密、调用 API 和已有原生桥 | 直接创建 StoragePartition、伪造平台能力、通过页面状态代替服务端鉴权 |
| `saas-server/` Go 服务 | 用户/会话/成员/账号权限、数据库、快照密文和版本、租约、审计、限流、后续配额/计费 | 运行本机 Tab、直接读取客户端 Cookie、解密账号快照、实现浏览器指纹覆盖 |
| 指纹 Chromium 与 `android-bridge/` 平台实现 | 原生窗口/Tab、账号固定分区、代理与指纹效果、实际存储读写、网络/文件/系统权限、来源白名单、端口和生命周期保护 | 复制 SaaS 登录、工作区、成员、套餐、计费或同步冲突业务到 WebUI / C++ / Java |

桌面 `chrome://fingerprint-manager` 是加载独立 SaaS 页面和传递受信消息的原生宿主，后续业务页面仍放在 `saas-web/`。即使历史 WebUI 中还留有账号或同步代码，也不据此继续扩展旧业务入口。Android 使用同一能力契约和真实平台适配，不另建一套 SaaS 业务。

云端账号目录、云端设备会话和本机运行 Tab 是不同状态：目录与会话来自 Go API，Tab 来自原生桥。`account_id` 是两侧稳定关联键；`tab_id` 是本机临时运行标识。目录删除、设备会话撤销与本地 Tab 关闭须分别执行自己的真实接口，不能互相冒充成功。

同步由 SaaS 前端安排读取、加密、租约与版本提交，Chromium 提供快照读写能力，Go 服务保存密文并检查云端权限与并发。HTTP 页面调用原生 `crypto` 只是通用算法回退，不把同步业务移入 Chromium。WebCrypto 加密或 Android Keystore 的存在也不能代替服务端授权。

### 5.2 修改归属判断表

**先确认已有桥能力是否足够：足够就修改 SaaS；需要改变实际浏览器行为、系统能力或原生安全边界时才修改 Chromium。** 新增原生能力或改变契约时两侧联动，不能只增加前端按钮或只改接口类型就宣称能力已实现。

| 需求 / 现象 | 应修改哪一侧 | 首先定位 |
| --- | --- | --- |
| 页面布局、中文文案、搜索筛选、选择、操作进度、弹窗 | SaaS 前端 | `saas-web/index.html`、`styles.css`、`app.js` |
| 登录表单、会话刷新、退出竞争、邀请/成员/设备列表交互 | SaaS 前端；接口规则变化再改 Go | `app.js`、`session-persistence.js`、`administration.js`、`operations.js` |
| 用户密码、权限/角色、账号目录、设备会话有效性、审计、限流、配额/计费 | SaaS Go 服务；需要交互时配套 Web | `saas-server/internal/httpapi/`、`auth/`、`config/`、`store/` |
| 打开环境按钮、复用已有账号 Tab、前端 Tab 状态刷新/失败提示 | 已有 `tabs` 足够时改 SaaS 前端 | `app.js`、`operations.js`、`bridge-contract.js` |
| 原生 Tab 重复创建、窗口/弹窗行为、控制台不可关闭、真实 Tab 压缩布局 | 指纹 Chromium；SaaS 仅配套展示 | 受管理 Tab、常驻启动及浏览器 UI 补丁、对应 `build/src/` 源码 |
| SaaS 页面里的运行环境 Tab 条样式 | SaaS 前端 | `app.js`、`index.html`、`styles.css`；与浏览器原生 TabStrip 分开判断 |
| 指纹输入表单、保存提示、重载提示 | 已有 `fingerprint` 足够时改 SaaS 前端 | `operations.js`、`app.js` |
| 实际 UA / UA-CH、Canvas / WebGL / 硬件值、Worker 指纹或代理效果 | 指纹 Chromium | `patches/extra/fingerprint/`、账号指纹/Worker 补丁、Android 原生配置与实现 |
| Cookie 串号、账号分区错误、实际存储读写/恢复或渲染器 IPC 异常 | 指纹 Chromium / Android 平台实现 | 账号 StoragePartition、存储补丁、`android-bridge/src/chromium/` |
| 同步类别、加密信封、合并交互、批量编排、导入导出 | SaaS 前端；云端版本/租约规则修改配套 Go | `snapshot-sync.js`、`app.js`、服务端快照/租约接口 |
| 新增 IndexedDB / Cache Storage 等快照能力或字段 | 两侧按契约联动 | 原生读取/写入 + 桥契约 + Web 加密/校验/合并；Go 按需要调整信封限制，不接触明文 |
| 业务 API 请求失败、目录为空、权限或版本冲突 | 先查 SaaS 请求/真实 API | Web 调用、Go 接口与数据库；不能仅因页面在 Chromium 中显示就改内核 |
| `http.request` 的真实代理/Cookie/重定向、文件授权、Keystore 存储失败 | 指纹 Chromium / Android 平台实现；参数传错时改 Web 调用 | 原生 HTTP、文件/SAF、`SaasSecureStorage.java` 和桥调用参数 |
| 普通浏览器 CORS 或静态托管配置 | SaaS Go 服务 / 部署配置 | `SAAS_ALLOWED_ORIGINS`、HTTP 服务与反向代理 |
| 原生桥来源授权、导航后撤权、端口断连、原生能力注入 | 指纹 Chromium / Android 宿主；客户端封装问题同时查 Web | 宿主 Origin 白名单、生命周期、`bridge-contract.js`、相关平台补丁 |
| 默认控制台地址或宿主加载流程 | 指纹 Chromium；服务地址迁移配套 SaaS 部署 | `chrome/common/chrome_switches.cc` 的 `kFingerprintSaasStartupUrl`、常驻启动补丁 |

### 5.3 每次开发的定位与联动步骤

1. 先写清触发动作与期望行为，再标明归属：SaaS 前端、SaaS Go 服务、Chromium 本地能力或桥接联动。按上表选入口，不因文件名含 `saas` 就把原生宿主当作业务代码。
2. 追踪真实链路：业务失败查看 Web → Go API → 权限/数据库；本地效果失败查看 Web 桥参数 → 来源/能力声明 → 原生宿主 → 分区/引擎/平台。先分清前端没发请求、API 拒绝、桥未连接、原生拒绝和执行效果错误，不用假状态掩盖缺口。
3. 能力声明不足或版本不支持时明确显示不可用。核对运行二进制是否包含目标实现；“源码已改”“接口可回读”“局部测试通过”均不能代替当前运行版实际效果验证。
4. 仅修改既有业务交互或 API 时，不改 Chromium / GN、不重新编译浏览器。按需要发布静态资源或重新构建并部署 Go 服务；本地 SaaS 归档继续使用固定文件名。
5. 改原生实现时，最小修改本地 `build/src/` 对应源码并同步可重放补丁；Android 对应 `android-bridge/` 实现与补丁须保持一致，新增补丁登记 `patches/series`。不能只把修改留在被忽略的展开源码中；共享原生修改同时评估 PC 与 Android 影响。
6. 新增或改变桥能力时同步 `bridge-contract.js`、`.d.ts`、`docs/saas-bridge-contract.md`、PC / Android 真实实现与能力声明。保持可选能力兼容，破坏性变更递增契约主版本；未实现的平台继续声明不可用，客户端不得假装成功。
7. 桥的原生来源/平台授权和 Go 的业务鉴权是两层独立检查，均须保留。SaaS 服务地址迁移要分别核对静态/API 地址、CORS、原生 Origin 白名单和固定启动地址，不通过放宽白名单代替配置一致性。
8. 验证按改动范围进行：Web 语法与相关回归、Go 相关测试及独立数据库集成、原生局部检查与对应运行效果。仅原生实现变化才安排必要的 Chromium 编译/链接；不能以 JVM 探针通过宣称 Android 平台验收通过。始终保留已有 Chromium 产物，先评估增量范围，避免无关全量编译。

### 5.4 PC 与 Android 的分工、差异和联动

**PC 和 Android 是同一产品的两个原生客户端，共用 SaaS 业务、账号/快照契约和部分 Chromium 引擎源码，各自实现平台宿主、Tab 管理、系统能力并独立构建验收。** 当前 PC 现场是 Windows x64；本文所述已验证桌面构建不代表 Linux/macOS 已构建或验收。Android 是 Chromium 的 Android 平台适配，不是把 Windows 程序或桌面 WebUI 直接装到手机。

两端连接路径如下；其中原生部分描述已编码架构，不表示最新二进制或 APK 已运行验收：

```text
PC：     同一 saas-web → bridge-contract.js → 固定 WebUI 宿主/消息处理 → C++ 账号 Tab 与平台能力
Android：同一 saas-web → bridge-contract.js → 主框架原生消息端口 → Java 宿主/TabModel → JNI/C++ 或 Android 系统能力
两端云端：saas-web → 同一 Go API → PostgreSQL；不会为 Android 复制一套登录/权限/同步后端
```

| 项目 | PC（当前 Windows） | Android |
| --- | --- | --- |
| SaaS 页面 | 独立 `saas-web/`，桌面布局 | 同一 `saas-web/`，移动响应式布局；页面样式变化通常仍只改 SaaS |
| 原生桥宿主 | `chrome://fingerprint-manager`；`chrome/browser/ui/webui/management/` 消息处理及 `chrome/browser/resources/fingerprint_manager/` 宿主资源 | `SaasBridgeHost.java`、可信主框架消息端口、Java 分发器；`TabModelJniBridge.java` 与 C++ JNI 接入 |
| 主要源码入口 | `build/src/chrome/browser/managed_tab/`、`chrome/browser/ui/webui/management/`、`chrome/browser/ui/views/` 及对应补丁；判断是否共享仍须查看条件编译/调用方 | `android-bridge/src/chromium/java/`、`native/` 及对应补丁；落地到 `build/src/chrome/android/java/src/` 与 `chrome/browser/ui/android/tab_model/` |
| 窗口与 Tab | 父浏览器窗口、原生 TabStrip、控制台关闭保护、账号 Tab 幂等 | Activity/任务栈、TabModel/TabCreator/TabRemover、活跃/冻结/归档 Tab、控制台保护；不能照搬桌面窗口对象与 TabStrip 实现 |
| 账号隔离 | 受管理账号持久 StoragePartition | 原生账号固定分区与真实 WebContents 所有权；不能用无痕模式或清空共享 Cookie 代替隔离 |
| 指纹/代理 | 桌面创建及网络/渲染器/Worker 配置链路 | Java/JNI 传递账号配置，再接入原生网络/渲染器/Worker；共享算法不证明 Android 配置传递或实际效果已通过 |
| 文件 | 系统权限允许的绝对路径；相对路径以配置目录 `SaasFiles` 为根 | 用户授权的 SAF 目录树 URI，页面只提供该目录内相对路径；不能接受 Windows 绝对路径或绕过系统授权 |
| 安全存储 | 当前 `secureStorage` 未接入，不能声称具有 Android Keystore 保护 | `SaasSecureStorage.java` 使用 AndroidKeyStore 与应用私有密文；不导出密钥、不保存 SaaS 业务规则 |
| 原生 HTTP | 桌面桥的真实原生网络处理 | `saas_native_http.h` / JNI / SimpleURLLoader 与账号分区；独立 JVM `SaasHttpClient` 不是 Android 后端 |
| 生命周期 | 浏览器窗口/Tab/WebContents 关闭、页面导航、渲染进程退出等 | 还须覆盖端口更换、Activity 生命周期、进程被系统回收、冻结恢复、SAF 撤权、设备密钥失效；不能只靠前台常驻 |
| 编译与产物 | 已有 `build/src/out/Development`、`Release`，程序/动态库及 `publish/` 运行目录 | 需要独立 Android 输出与目标架构、Java/JNI/C++ 编译、Android 原生库、APK/AAB 打包签名；现场尚无已验收的 Android 输出 |
| 验证要求 | 对包含最新源码的 PC 程序验证白名单、Tab、隔离、网络、指纹及系统文件 | 独立 JVM/协议检查之后还须完整 Android 编译与真机/模拟器测试，包括权限、网络、存储及生命周期；PC 通过不能替代 Android 通过 |

两端契约一致指字段、语义和错误边界一致，不要求系统机制或能力额度完全相同。文件路径模型、文件/HTTP 容量限制、`secureStorage` 支持情况等差异见 `docs/saas-bridge-contract.md`。前端依据真实能力声明分支，不仅按设备名称或 UA 判断；新增可选能力不要求未实现端伪造支持。

#### 平台修改判断

| 需求 | 修改范围与验证 |
| --- | --- |
| 手机上的 SaaS 按钮拥挤、弹窗或列表布局不合适 | 先改同一 `saas-web/` 的移动样式与交互；没有原生行为变化就不改 Android Java/JNI，也不编译 APK |
| PC 原生 TabStrip、桌面窗口或管理页宿主问题 | 改 PC 相关源码/补丁，验证 PC；同时核对触及文件有无共享接口/Android 编译分支 |
| Android 控制台启动、冻结 Tab、端口断连或 SAF/Keystore 问题 | 改 Android 宿主、模型、JNI/平台实现及补丁；平台问题须以 Android 构建和设备效果验收 |
| UA/UA-CH、硬件值、Canvas/WebGL 或 Worker 共享实现变化 | 检查 `chrome/browser/chrome_content_browser_client.*`、`content/`、Blink、指纹补丁等实际调用链，分别评估 PC/Android 注入与效果；不能只凭补丁名归类 |
| 默认 SaaS 地址变化 | 修改共享 `chrome/common/chrome_switches.cc` 常量和对应持久补丁；PC 编译链接，Android 编译原生库后重新打包签名，各端独立生效 |
| 桥接口、能力语义或加密快照格式变化 | 同步 SaaS、契约和受影响的 PC/Android 实现；做兼容性验证，记录未实现端的能力状态 |
| 只修复某端实现，不改变公共契约 | 保持另一端接口兼容，做必要的共享代码回归；不无故重写另一端或触发其全量构建 |

补丁名包含 `android` 不保证只修改 Android 文件：例如账号环境补丁也涉及共享 `chrome/browser/chrome_content_browser_client.cc`。评估影响必须看补丁实际文件、`BUILDFLAG(IS_ANDROID/IS_WIN)` 等条件分支、构建源清单与调用链；共享源码存在不意味着所有功能都自动跨平台生效。

#### 平台源码与构建约束

- Android Java 宿主在 `android-bridge/src/chromium/java/` 保存，对应展开源码在 `build/src/chrome/android/java/src/org/chromium/chrome/browser/saas/`；平台无关加密/来源/分发代码从 `android-bridge/src/main/` 对应到展开源码的 `com/fingerprint/saas/bridge/`。原生头文件对应 `chrome/browser/ui/android/tab_model/`；挂接点还包括 `TabModelJniBridge.java`、`TabModelImpl.java`、`tab_model_jni_bridge.cc` 和 `chrome/android/java_sources.gni`。已有文件最小修改，保持模块副本、展开源码和持久补丁一致；本地缺少的上游文件不得伪造后宣称已编译。
- `build-configs/common.gn` 当前含 Windows/D 盘工具链配置和 `target_cpu="x64"`，`prepare_build_mode.ps1` 默认准备现有 PC 输出；不能直接复制这些参数作为 Android 配置。Android 构建须使用独立输出、明确 Android 目标与 ABI、SDK/NDK/Java 及可运行构建宿主，复用依赖但不覆盖 PC 输出。本次只写指引，不创建 Android 构建目录或修改 GN。
- `utils/check_cpp_syntax.py` 当前仅用于 Windows 上已有 Chromium 构建规则；即使某个共享/Android C++ 文件借助桌面参数通过局部检查，也不能记作 Android 工具链编译通过。Java 语法解析、JNI 生成、JVM 通过同样不等于 Android 类型检查、链接或设备验收。
- PC 变更记录注明配置、目标程序和运行验证；Android 记录注明 ABI、Java/JNI/C++ 编译、APK/AAB 与设备验证。原生更新必须进入各端实际二进制才生效；`output/saas/fingerprint-saas.zip` 只发布 SaaS 服务与页面，不更新 PC 内核或 Android APK。
- 完整保留两端已有源码、工具链、下载缓存、构建图、对象、生成文件和发布产物；Android 构建/平台测试不使用逆向工具流程。GN/公共构建配置变化先评估影响，不能为文档、业务页面或某端小修清空另一端缓存。

默认地址当前为 `http://127.0.0.1:8787/`；Android 的回环地址指向设备自身，部署联调须改为设备实际可访问的服务地址并保持客户端来源白名单一致。服务端 `SAAS_ALLOWED_ORIGINS` 只控制 API CORS，不能授予原生桥权限。

## 6. 开发、验证与发布

1. 先读本文件，再读目标模块契约。最小修改、保留中文注释和未完成改动，不用假数据替代业务；生成文件采用 UTF-8，优先无 BOM。
2. SaaS 业务修改优先留在独立 Web / Go 服务。前端当前不需要 Node 构建或额外 UI 框架；浏览器直接加载静态资源。
3. 日常使用 Development 和已有编译参数，局部验证工具为 `utils/check_cpp_syntax.py`。该工具不等于完整编译/链接；不主动改变 GN 公共参数或执行 Release 全量构建。
4. Android 构建、SDK/NDK、GN/Ninja 与平台测试按构建任务处理，不读取逆向工具说明、不调用逆向工具链。
5. Go 验证在 `saas-server/` 执行 `go test ./...` 与 `go vet ./...`；真实数据库专项仅使用独立 `SAAS_TEST_DATABASE_URL`。跳过集成测试须明确记载，不把包级通过当作数据库通过。
6. Web 验证执行 `node --check` 和 `node --test saas-web/*.test.cjs`；测试探针只属于测试，不进入最终运行包。Android 独立测试入口见 `android-bridge/README.md`，JVM 通过不等于设备通过。
7. SaaS 部署要求 Go 1.24+、PostgreSQL 14+，提供 `SAAS_DATABASE_URL` 和至少 32 字节的 `SAAS_JWT_SECRET`；`SAAS_WEB_DIR` 指向静态资源目录，生产通过 HTTPS 反向代理。首个用户由受控 `bootstrap-user` 创建，不内置账号。

### 固定名称打包

仓库现场未发现已有 SaaS ZIP 或服务端打包文件。新增独立入口 `utils/package_saas.ps1`，首次默认归档为 `output/saas/fingerprint-saas.zip`；如果已有包位于其他路径，传入 `-OutputPath` 保留原完整文件名，后续每次继续使用同一参数。

```powershell
pwsh -File utils/package_saas.ps1
# 已有固定名称时使用其原路径，脚本不会追加日期或版本号：
pwsh -File utils/package_saas.ps1 -OutputPath 'output/saas/fingerprint-saas.zip'
```

归档包含实际 Go 可执行文件、前端运行资源、部署说明与本项目总览；不包含密钥、数据库、测试、Chromium、编译对象或假数据。默认编译当前 Go 目标平台；跨平台可通过 Go 标准 `GOOS` / `GOARCH` 配置，包内 Windows 程序名为 `fingerprint-saas.exe`，其他平台为 `fingerprint-saas`。

本文件的目录树和详细资料链接描述源码仓库；运行归档不附带整个仓库，进一步开发时回到源码目录查看对应协议与实现。

打包先在独立临时目录完成编译和归档，再替换固定目标；失败保留旧包。替换时旧包移入 Windows 回收站，临时目录也移入回收站，不在项目中累积日期版本。该脚本的回收站操作限定 Windows 主机，编译目标可不同；它不承担服务器部署或 Chromium 发版验收。

## 7. 本次整理范围与保护规则

- 已确认的历史文件：`.playwright-cli/` 下 42 个页面快照、截图和控制台日志，共 379,828 字节；无源码/文档引用、未被 Git 跟踪，整目录移入 Windows 回收站，可从回收站恢复。
- 保留 `output/playwright/saas-members-verification.png` 作为最终验证截图；保留原进度、路线、协议、设计、历史报告与上游说明，统一通过本文导航，不删除有效事实和中文描述。
- **完整保留 `build/`、`publish/`、Chromium 源码、Development / Release 输出、GN/Ninja 图和依赖记录、对象/生成文件、工具链、下载/域替换/包缓存及现有测试配置。** 不用“重新编译可生成”作为清除理由。
- 现场已有 Android 宿主及多份 Chromium 补丁未提交改动，整理仅修改文档与独立打包入口，不回退这些变更。
- 今后清理只处理经核实的历史验证文件、废弃临时文件和成功替换的 SaaS 旧包；先核对引用、运行占用和绝对路径，再送回收站。禁止宽泛递归删除或进入 Chromium 输出清理。

## 8. 后续优先顺序

1. 在保留现有缓存的前提下核对 Development 构建图、工具链和实际增量范围，安排最新 Chrome 链接；不再沿用“图只有 558 字节”的旧结论。
2. 对最新 PC 二进制做常驻控制台、来源白名单、同账号幂等 Tab、单页跳转、跨账号隔离、文件/HTTP、Worker 请求头与硬件实际效果验收。
3. 联调真实 HTTPS 服务、PostgreSQL 与多设备加密同步，覆盖租约、权限、版本冲突、失权和断网恢复。
4. 准备 Android 可运行构建环境，完成完整 Java/C++ 类型检查、APK/AAB 和设备测试，重点验证 SAF、Keystore、分区网络与生命周期撤权。
5. 独立设计其他存储类别同步、配额/保留/计费与生产部署边界；异常关闭自动恢复继续按已有记录保持低优先级。
6. 完成整体功能验收后再执行 Release 发版编译、安装升级与发布回归。此次整理和 SaaS 归档不代表上述步骤已完成。

## 9. 详细资料入口

- [项目进度与历次局部验证](docs/implementation-status.md)
- [目标与分阶段路线](docs/managed-tab-roadmap.md)
- [快照、权限、同步与恢复契约](docs/saas-account-sync.md)
- [原生能力、消息、安全与平台边界](docs/saas-bridge-contract.md)
- [服务端 API 定义](docs/saas-api.openapi.yaml)
- [服务部署、接口与限流](saas-server/README.md)
- [独立 Web 使用与验证](saas-web/README.md)
- [Android 实现与局部测试](android-bridge/README.md)
- [构建模式](docs/build-modes.md)、[常驻启动地址](docs/resident-saas-startup.md)
- [商业页面设计](docs/saas-product-design.md)、[浏览器壳计划](docs/saas-browser-shell-plan.md)
- [2026-10-02 旧 Development 隔离报告](docs/fingerprint-tab-test-report.md)
