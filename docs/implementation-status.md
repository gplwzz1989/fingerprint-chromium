# 指纹 SaaS 浏览器阶段总结

## 2026-10-05 指纹统一契约修正后的当前状态

- PC 与 Android 的账号指纹参数按统一契约处理：`fingerprint_seed`、`fingerprint.user_agent`、`fingerprint.hardware_concurrency` 使用相同字段、范围和默认语义。平台缺失能力作为实现缺陷，不作为正常兼容差异。
- Web 恢复选项取操作开始时页面选择与云端快照类别的交集；选择指纹时检查目标桥能力，应用原值后回读账号、种子、UA 和硬件并发数，失败则在导航网站或写入存储前停止。Worker 运行验收标记只记录事实，不阻断合法参数恢复。
- 本轮验证 Web 41 项回归、业务 JavaScript 语法、Go 非缓存测试和 `go vet` 均通过。未触发 Chromium/Android 编译，未改变编译参数；真实页面、网络请求、Worker 效果和设备验收仍待平台任务完成。

## 2026-10-05 独立环境控制的两份备选计划

- 按用户要求新增 [方案一：保留 postMessage，增加 Socket.IO](browser-control-hybrid-plan.md) 和 [方案二：统一使用 Socket.IO](browser-control-socketio-plan.md)，分别记录目标架构、共同控制契约、DOM/网络能力、原生与 Go 分工、工程选型、授权/隐私、并发/断线、迁移回退、性能测试及 PC/Android 验收条件；在总览加入导航。
- 两份方案都是待选计划，没有修改现行控制协议或宣布实现完成。共同原则为一套控制契约和一个原生执行模块；方案一保留内嵌本地桥，方案二最终迁移全部控制调用到 Socket.IO，并要求本机端点保留断公网时的本地控制路径。
- 本次源码核对确认现有桥未提供 DOM 查询或页面网络监听；PC `http.request` 使用 Profile 网络工厂，未按 `tabId` 选择账号独立分区，计划将此列为需修复项。核对对象为源码/契约，不代表运行版已验收。
- Socket.IO 官方 C++ 库是客户端，本机服务端和 Go 现代协议兼容性须先验证；不直接采用已归档的旧 Go 库。性能结论只列路径推断和对照压测方法，没有虚构延迟、吞吐或通过结果。云端敏感载荷、移动端后台和 HTTPS 回环接入各有明确门槛。
- 验证范围为本次新增 Markdown 的 UTF-8 编码、相对链接、必需章节及增量差异检查；未修改业务/原生代码、编译参数或构建产物，未执行 GN/Ninja、依赖安装、部署、发布或设备测试。既有未提交变更保留。

## 2026-10-05 T01 两端统一指纹参数与恢复门槛纠正

- 用户明确 PC/Android 指纹参数不应不兼容。已核对当前两端的种子、UA、硬件并发数字段及数值范围，SaaS 按共同契约校验数字种子、UA 的可打印 ASCII 和字段范围；未知字段、控制字符与越界输入不进入恢复。两端新增字段须同步实现，平台缺失是实现缺陷，不能用平台降级替代。
- 移除上一阶段由 Web 拼装的 `fingerprint_contract` 和 Worker 验收标记阻断逻辑。已有快照中的该字段可作为历史信息保留，但不作为恢复门槛；不把任何原生 `worker_fingerprint_verified` 改成 true，也不宣称 Worker 效果通过。
- 选中指纹恢复时，在改变环境前检查实际目标桥能力；应用后通过 `fingerprint.get` 回读账号、种子、UA、硬件配置。在导航网站和写入存储前，遇到漏写、错账号或静默改值明确停止。配置回读通过只证明参数应用，页面/请求/Worker 实际效果仍须逐端验收。
- 恢复类别改为云端所含类别与操作开始时页面选择的交集；操作途中改变选择不扩大范围，未选指纹不修改本地参数。旧快照无需新增诊断元数据即可恢复合法参数。
- 验证：业务 JavaScript 与测试文件语法检查通过，全套 Web 回归 41 项通过。覆盖无诊断元数据的恢复顺序、Worker false 不阻断、未知字段/非法参数、目标能力缺失、四种回读错误停止副作用、类别选择及操作中选项变化。桥对象仅作为明确测试边界，快照加解密真实执行；未修改原生源码或编译参数，未运行 Chromium/Android 编译或设备验收。
- 同步更新项目总览、开发规则、开发计划、同步/桥协议与 `AGENTS.md`。下文 2026-10-04 T01 的 Worker 阻断方案是历史过程，已由本节取代。

## 2026-10-05 计划状态核对

- M0：T01 与 T03 已完成局部实现和回归；T02 的 `build-compatibility.patch` 已通过格式/当前源码反向检查，`missing-dependencies.patch` 仍因第 108 行损坏保持阻塞，未改动其既有用户修改。
- M1：Windows Release 最新原生产物和壳/隔离运行验收仍待安排，当前程序日期早于最新 SaaS 代码。
- M2：T06 工具链和 GN 图通过；T07 已完成 Android Java/JNI/C++ 集成、资源生成、链接和 `chrome_public_apk`，APK 为 376,929,667 字节、SHA-256 为 `cb26e45ec73444063ac5447390944cbe5e4657dadbbba6b6c0bbbc76d20d4704`；T08 真机安装、权限、生命周期、网络和运行效果仍未完成。
- M3：真实 HTTPS、多设备 PostgreSQL、权限撤权和审计闭环仍待独立环境；本地 Go 集成测试未配置 `SAAS_TEST_DATABASE_URL` 时保持跳过。
- 快照写入的 `snapshot_written`/`snapshot_overwritten` 审计已移入同一 PostgreSQL 事务；审计写入失败会回滚快照，避免出现快照成功但审计缺失。其他本地导出/恢复动作仍需真实服务审计验证。
- M4/M5/M6：跨设备闭环、安装升级/发布、商业运营能力、扩展存储和 Linux/macOS 仍按依赖顺序保留，未因局部通过提前标记完成。

## 2026-10-05 安卓高保真图纳入开发计划参考

- 已将 `design/android-saas-multitab-hi-fi.png` 链接和预览加入 `docs/development-plan.md` 的 M2，关联 T07 界面交付与 T08 设备验收；明确 SaaS 移动页面和 Android 原生外壳各自归属。
- 补充固定首页、账号标签切换/关闭、返回控制台、多标签/窄屏及登录/加载/空目录/失败状态的验收要求。设计参考不代表已实现，不扩大构建或参数修改授权。
- 验证范围为文档引用与差异检查；未修改业务代码、编译参数或构建产物，未执行平台构建和设备测试。

## 2026-10-05 安卓 SaaS 启动与多标签高保真设计预览

- 使用内置图片生成工具制作 `design/android-saas-multitab-hi-fi.png`，展示启动进入 SaaS 首页、账号网页浏览与原生多标签总览三种设计状态；生成提示词保存在同目录的 `android-saas-multitab-hi-fi-prompt.txt`。
- 沿用现有浅色 SaaS 风格；固定首页不提供关闭入口，账号标签可切换与关闭。未读取的账号、网站与指标使用空值和版式占位，不生成业务记录或成功结果。
- 验证对象为生成图片，已检查整体布局、中文文字与固定首页关闭入口；这是目标设计预览，不是安卓实机截图，不代表新增原生视觉已实现或设备验收通过。本次未修改业务代码、编译参数或构建产物。

## 2026-10-05 T06 Android 独立配置与 WSL 真实编译验证通过

- 用户明确同意此前提交的具体参数修复方案。已补齐 `build-configs/android-arm64.gn` 中实际 Linux 工具路径和全部既有生效参数，原地更新活跃 `/home/gaoyang/chromium-build/AndroidDevelopment/args.gn`，移除对 PC `common.gn` 的导入；JDK 登记改为实际源码内 Linux 23.0.2，SDK/NDK、nightly Rust、功能开关及优化值不变。
- 新增 `utils/prepare_android_build.py`：仅 WSL 可用，检查实际工具、版本、SDK/NDK、Android Rust 标准库、独立 GN 和源码根目录；默认只检查模板一致性，显式 `--apply` 才原地写入已确认内容，相同内容不写入。不运行 GN/Ninja、不修改环境变量或下载工具。PowerShell Android 模式已在写入前拒绝。
- 原地 GN 重生成成功：57,663 个目标、4,068 个输入文件。逐项比较前后 1,203 项生效参数，无变化；完整目标的 56,547 条命令 SHA-256 相同（`e22394ba302831fd3628a793c90eb967f075bbe01a5569f681153b33b793144f`）。本次配置变更造成的额外 C/C++、Rust 编译或生成/链接命令变化为 0，不代表既有源码变更的待执行量为 0。
- 真实编译验证：从 Ninja 图提取 `obj/base/base/check.o` 的 Android ARM64/API35 完整命令，实际编译现有 `base/check.cc` 返回 0，仅将验证对象/依赖文件写到原对象目录的独立文件；产物 30,520 字节，ELF 机器类型 AArch64。原对象和依赖缓存未覆盖；未启动新全量构建。第一次验证命令筛选包含宿主依赖而未执行编译，改为精确目标筛选后通过。
- 当前验证边界为工具链/配置/实际单编译单元通过，完整 APK、完整链接和设备验收未完成。原 PC/Android 输出、混合 GN 历史缓存和其他任务变更保留；本次提交仅包含本修复范围。

## 2026-10-05 T07 Android ARM64 资源依赖、链接与 APK 构建验证

- 首轮全量续编在 Safe Browsing 资源处失败：Android `safe_browsing_mode=0` 下，已有 Android `file_type_policies` 源码依赖没有同步生成 `download_file_types.pb`，导致 `IDR_DOWNLOAD_FILE_TYPES_PB` 未声明。补齐 Android 资源生成/打包条件，未修改 `args.gn`、ABI、工具链或优化参数。
- 后续链接检查发现两个源码一致性问题：JNI 存储请求参数使用 `bool` 而生成声明使用 `jboolean`；Android `ManagedTabContext` 缺少 `WEB_CONTENTS_USER_DATA_KEY_IMPL`。已做最小源码修复，并登记 `android-link-fixes.patch`；同时补回 Chromium 144.0.7559.132 对应的 `chrome/android/proguard/main.flags`，登记 `android-proguard-main-flags.patch`。
- 可重放补丁为 `android-safe-browsing-resources.patch`、`android-link-fixes.patch`、`android-proguard-main-flags.patch`，均在当前展开源码上反向校验通过，并已加入 `patches/series`。局部对象编译和资源生成通过。
- 最终使用 WSL Ubuntu、既有 `/home/gaoyang/chromium-build/AndroidDevelopment`、`ninja -j 4 chrome_public_apk` 构建退出码为 0。APK：`/home/gaoyang/chromium-build/AndroidDevelopment/apks/ChromePublic.apk`，大小 376,929,667 字节，SHA-256 为 `cb26e45ec73444063ac5447390944cbe5e4657dadbbba6b6c0bbbc76d20d4704`，生成时间 2026-10-05 16:54:41（Asia/Shanghai）。尚未进行安装、设备权限/生命周期、网络、SAF、Keystore 或运行效果验收。

## 2026-10-04 T06 Android 工具链修复：参数确认前的入口保护记录

- 已原地修改 `utils/prepare_build_mode.ps1`：Android 模式在目录创建/配置同步前返回中文错误，停止将不完整模板写入旧 `out/AndroidArm64`；PC Release 行为保持不变。构建指引已改为现有 WSL 活跃输出，并明确实际 JDK 23.0.2、Linux nightly Rust/bindgen 与独立 Linux GN 的调用关系。
- 修正上一轮审查边界：nightly Rust 是源码支持的自定义工具链，现有包包含 Android ARM64 标准库；另一个已有 Chromium Rust 包不含该标准库，不能仅替换路径解决问题。本阶段不切换 Rust、不启用/关闭其他功能或检查。
- 只读命令图统计基线：`chrome_public_apk` 包含 36,768 个不同 C/C++ 源文件、41,768 个 C/C++ 编译单元、173 个 Rust 编译单元、11 个 bindgen 动作、12,439 个其他生成动作、2,130 个归档动作和 26 个链接动作；本阶段未执行 Ninja 干跑、GN 重生成或编译。当时提交的模板/登记/WSL 配置入口方案及预计影响见 `docs/build-modes.md` 的修复表，后续确认与验证见本文最新记录。
- 验证：PowerShell 语法检查通过，Android 入口实际执行并在写入前拒绝；7 个相关配置/构建图的哈希及时间戳未变化。已有其他流程的 Android 构建未被停止或修改，本阶段的局部通过不代表该构建、APK 或设备验收通过。

## 2026-10-04 WSL Android 原地续编与持续监控

- 用户要求直接使用既有 WSL 工具链，并提交、推送本次修改；Ubuntu 已核实为 Running。旧 Android 输出 `/home/gaoyang/chromium-build/AndroidDevelopment` 有编译历史和依赖缓存，保留其原参数与产物继续构建，不复制到 PC 或新输出。
- 新 `out/AndroidArm64` 的只读预检失败于 Linux `bindgen` 缺失：其图指向包含 Windows `.exe` 的仓库工具目录。WSL 中已有可执行 Linux Rust/bindgen；根因是构建入口未复用既有工具路径，不是 WSL 工具链未安装。
- `build-configs/build-roots.json` 的 Android 输出登记改为既有 Linux 目录；未改变其 `args.gn`、工具链版本、优化或功能参数。原地 GN 重生成通过：57,663 个目标、4,069 个文件；现有图变化需要重新核对实际增量范围。
- 本聊天每 5 分钟监控已启用，跟踪既有输出的进程、日志和退出码；实际错误先局部修复并验证，仅提交本次修改后推送，再继续构建。原有用户改动与全部 PC/Android 缓存保留；编译和设备验收尚未通过。

## 2026-10-04 T03 快照版本覆盖收口

- 服务端 `PUT /api/v1/accounts/{account_id}/snapshot` 现在拒绝 `If-Match: *`；包括明确覆盖在内，必须使用刚读取的具体版本。版本不匹配始终返回 `snapshot_revision_conflict`，成功的明确覆盖继续记录 `snapshot_overwritten`。
- 同步契约、服务端 README、开发计划和集成测试已同步。现有 Web 客户端本来就提交具体版本，本次没有改变前端调用格式，也没有修改数据库结构或编译参数。
- 局部验证：`saas-server` 的 `go test -v ./...` 与 `go vet ./...` 通过；`TestParseIfMatch` 覆盖通配版本拒绝。网页 37 项回归及业务 JavaScript 语法检查通过。真实 PostgreSQL 集成仍依赖 `SAAS_TEST_DATABASE_URL`，本轮未配置时标记跳过。

## 2026-10-04 T01 指纹跨平台恢复前置检查

本节方案已于 2026-10-05 按用户澄清纠正，当前行为见本文顶部统一参数记录；保留下列历史实现与当时测试事实，不作为当前恢复门槛。

- `saas-web/snapshot-sync.js` 在上传时从真实 Tab 列表记录 `fingerprint_contract`，包含当前支持的指纹字段、Worker 配置范围/生命周期和 `worker_fingerprint_verified` 状态；合并时保留该合同。
- 指纹恢复在创建或导航账号 Tab 前执行严格预检：合同缺失、字段不完整或 Worker 效果未验证均拒绝恢复；不影响取消指纹类别后的其他同步类别。该检查不伪造平台能力，也不替代 Windows/Android 最新产物验证。
- 新增网页回归覆盖未通过完整指纹效果验证时不创建目标 Tab。全套网页回归由 36 项增加到 37 项，`snapshot-sync.js` 语法检查通过；Android/PC 原生能力合同的真实来源和最终 `worker_fingerprint_verified=true` 仍待平台任务验收。

## 2026-10-04 T02 补丁格式与当前源码反向检查

- `patches/upstream-fixes/build-compatibility.patch` 的格式问题已最小修复：删除两个不含增删内容的伪 hunk，并补齐一个 hunk 内的空白上下文；没有改动有效代码行。`git apply --numstat` 通过，在当前 `build/src` 展开源码上 `git apply --reverse --check` 通过，说明当前源码已包含该补丁的有效改动。
- `patches/upstream-fixes/missing-dependencies.patch` 仍在第 108 行报告损坏；该文件已有用户未提交修改，本轮没有猜测删除或重写上下文，保留为 T02 的阻塞项。
- 补丁检查没有触发 GN/Ninja、源码重解包或编译；补丁文件中的前导空白属于 Git patch 语法，`git diff --check` 会将该必要空白提示为 trailing whitespace，不代表补丁正文新增代码空白。

## 2026-10-04 桌面浏览器壳设计与源码状态核对

- 对照 `docs/design/saas-browser-shell-desktop.svg` 确认：启动管理宿主、按默认地址全屏加载独立 SaaS、原生固定/关闭保护及账号 Tab 幂等打开已有源码；不能全部列为从零开发，也未完成最新运行版验收。
- 当前 pinned 原生管理标签与图中宽标题标签、当前 252px 单侧栏与图中两级侧栏存在差异；图中 SaaS 标签的“×”还与不可关闭要求冲突。保留设计原文件，浏览器壳计划增加逐项状态，开发计划 T05 补充视觉收口与验收。
- Release/publish 产物日期仍为 2026-09-30，Development 为 2026-10-02。本次仅源码/文档/产物元数据核对，未启动客户端、修改源码或执行编译。另观察到 Android `build.ninja` 已于 2026-10-04 22:30:28 生成，旧“没有图”的现场快照已变化；未核对其完整编译或 APK，不能据此记作 Android 通过。

## 2026-10-04 退出与失权后的本机环境策略确认

- 用户确认：退出 SaaS、设备会话撤销或账号权限被移除后，停止旧身份的 SaaS 云端业务，已打开的本机账号 Tab 保留并可继续浏览、导航及访问网站，不自动关闭或清空分区。
- 已同步总览、开发规则、开发计划 T01/T10/A11 与 `AGENTS.md`；在途同步/恢复取消限定到相关业务和恢复副作用，独立浏览不受影响，原生来源/系统权限仍须校验。远端撤权在设备获知后处理，不承诺离线立即生效。
- 本次仅补齐文档中的产品决策与验收条件，未修改运行逻辑、构建参数或产物；停止云端业务与保留本机浏览的完整运行验证仍列为 T10/A11 待办。

## 2026-10-04 跨平台项目分析与三份持续迭代文档

- 用户确认首版范围为 Windows + Android，Linux/macOS 后续规划；优先打通真实多账号隔离与双向云端同步。跨平台指纹参数必须全部严格复现，不兼容拒绝恢复，不静默降级；本阶段结束时退出/撤权后的本机 Tab 策略尚待确认，后续确认见本文顶部记录。
- 原地补充 `PROJECT-OVERVIEW.md` 的产品决策、架构/数据流、现场状态与风险；新增 `docs/development-rules.md` 和 `docs/development-plan.md`，形成长期规则、里程碑、13 项任务与 16 项真实验收矩阵。`AGENTS.md` 增加阅读入口，编译参数强制锁原条款保持不变。
- 分析核对 SaaS Web/Go/数据库、Windows 宿主与 `ManagedTabContext`、Android Java/JNI/原生模块、共享指纹/Worker、补丁、构建参数和发布脚本；本轮只修改文档，不实施分析中列出的源码修复。
- 当前现场：Release `build.ninja` 为 7,008,931 字节，写入于 2026-09-30 23:44:49；实际 `args.gn` 于 2026-10-04 21:54:04 写入。Release 与 publish 程序仍为 2026-09-30，Development 为 2026-10-02；本次未重生成构建图、编译或链接，不能据图存在记作最新原生通过。
- Android 活跃输出已切换并登记为 WSL 的 `/home/gaoyang/chromium-build/AndroidDevelopment`；该目录已有 `build.ninja`，原地 GN 重生成记录为 57,663 个目标、4,069 个文件。Windows 侧 `out/AndroidArm64` 预检失败于工具路径不匹配，不能作为活跃入口。WSL Ubuntu Running/WSL2、SDK/NDK 28.0.13004108、JDK、Ninja 1.11.1 和 OpenJDK 17.0.20.1 已核实；上述构建图与工具可用性不等于 Android 编译、APK 或设备通过。历史 WSL 不能启动不再作为当前结论。
- 补丁只读 Git 格式检查：`build-compatibility.patch` 第 1601 行、`missing-dependencies.patch` 第 108 行失败；Android 原生端口补丁格式检查通过。后者失败文件已有其他任务未提交修改，本次未触碰。此结果不代表项目自有补丁工具全量重放检查，也未运行完整重放。
- 优先风险已列入计划：快照提交后单独写审计；本机退出/撤权语义；恢复首请求与部分写入；严格指纹兼容检查；默认回环地址与真实设备可达服务配置。`If-Match: *` 收口已完成，真实 PostgreSQL 竞争与审计仍需独立测试库验证。
- 本次验证：Web 36 项回归及全部业务 JavaScript 语法检查通过；Go `go test -v ./...`、`go vet ./...` 通过（Go 包测试含缓存），PostgreSQL 专项因未配置独立测试数据库跳过。Android/JVM、原生编译、设备、实际桥、跨平台运行及生产发布未复验。文档完成不等于 T01～T13 实现完成。
- 未修改任何构建参数、环境配置、工具链、源码、补丁或运行产物，未安装工具、执行 GN/Ninja、启动 Chromium 构建或发布；已有输出/缓存和用户改动全部保留。当前后续入口以开发计划为准，下文“本轮”等表述均属于对应历史阶段。

## 2026-10-04 Android 编译宿主改为 WSL

- 按用户要求，Android ARM64 的 GN/Ninja、SDK/NDK/JDK、Gradle 和 APK/AAB 编译统一改为在 WSL Linux 发行版内执行；Windows PowerShell 仅用于检查 WSL 状态和查看结果，不再作为 Android 编译入口。
- WSL 编译使用 `/mnt/f/mywork/chrome-finger`、仓库现有 Linux GN/Ninja、`/home/gaoyang/Android/Sdk`、NDK 28.0.13004108、`/usr/lib/jvm/java-17-openjdk-amd64` 和独立 Android 输出/缓存；不得复用 Windows PC Release 输出或 C/D 盘 SDK/NDK/JDK 参数。启动前必须确认 WSL 发行版 Running、Linux 工具链版本和 Android 构建图。
- 本次 Windows 侧尝试仅生成了 `build/src/out/AndroidArm64/args.gn`，随后因从 Windows 调用 GN 且未传入 Chromium 源码根目录而停止，未执行 Android 编译；该输出目录不代表 Android 构建图或 APK 已生成。

## 2026-10-04 PC 仅保留 Release 配置

- 按用户明确要求移除 `build-configs/development.gn`，原文件放入 Windows 回收站；`prepare_build_mode.ps1` 移除 `development` 模式，`build-roots.json` 移除 PC 开发版活跃路径，保留 Release 与 Android 独立配置。
- `utils/check_cpp_syntax.py` 默认输出改为 `build/src/out/Release`；同步项目规则、总览与当前构建指引。PC `common.gn` 和展开副本仅更新两行模式描述，Release 参数、Android 参数、工具链及独立缓存路径值不变。
- `build/src/out/Development/args.gn` 停止由 Git 跟踪，但其磁盘文件与原 Development 构建图、对象、依赖缓存、二进制均保留；下文 Development 验证和两模式记录属于历史事实，不作为后续构建入口。
- 本次不执行配置准备脚本、不覆盖实际 Release 参数、不进行 GN 重生成或 Chromium 编译，因此未触发源码重编译；Release 图是否包含最新告警参数仍沿用上一节的待重生成边界。
- 验证：PowerShell 入口语法和旧开发模式拒绝检查通过，Python 语法与 Release 默认目录检查通过，路径清单仅保留 PC Release 且 Android 不变；Release/Android 参数值及原有构建图、依赖记录和缓存文件内容校验通过。入口拒绝检查在参数绑定阶段退出，未执行配置同步。

## 2026-10-04 已确认的告警处理配置

- 用户已确认所有告警忽略、真正错误继续阻断：共享 `default_warnings` 增加 C/C++ `-w` 和 Rust `-Awarnings`，同步展开源码、独立补丁及 `patches/series`；保留已有 Rust 明确错误级别的检查，不使用会降低所有错误级别检查的 `--cap-lints`。
- PC `common.gn` 与展开副本增加 `fatal_linker_warnings=false`，Release 现有实际参数仅增加同一项；Development 继续导入共用副本，保留模板与实际参数的其他差异。Android ARM64 独立模板增加 `treat_warnings_as_errors=false`、`fatal_linker_warnings=false`，使 Java/D8/R8 等现有构建规则不再因告警升级而失败。Java和链接工具仍可输出非阻断提示。
- 本次确认仅授权上述告警配置，未改变工具链、ABI、优化、组件或链接模式，也不授权后续其他参数变化或全量构建。此前只读图分析的 PC `chrome` 参考范围约 4.3 万源文件、4.4 万 C/C++ 编译单元；新增 Rust 与链接配置的准确增量未确定，Android 无现成构建图，影响量未知。
- 配置将在对应构建图下一次重生成后进入实际命令；本次不执行 GN 重生成或 Ninja 构建，不启动 Chromium 全量构建，不修改或清理现有构建图、对象、依赖缓存和发布文件。
- 局部验证：现有 Clang/Rust/LLD 的 10 项独立探针通过，覆盖告警抑制、链接告警非阻断、C++ 未声明标识符、Rust 类型错误、链接缺失符号及 Rust 明确错误级别检查保留；5 个配置文件的 GN 语法解析和补丁反向应用检查通过。验证未使用 Chromium 全量目标，Android 完整编译与设备验收仍未执行。

## 2026-10-04 编译参数强制锁

- 新增修复原则：PC / Android Chromium 优先局部定位和最小修复，禁止擅改全局或编译参数解决局部问题；确需变更必须先说明原因、局部方案不足及预计重编译文件量，取得用户明确确认后实施。
- 已在 `AGENTS.md` 写入 PC / Android 同等适用的强制规则：编译参数默认锁定，修改前必须报告参数差异、受影响平台/输出、预计重编译源文件/编译单元数量及估算依据，并取得用户针对本次变更的明确确认。
- 覆盖配置同步、工具链/环境覆盖及输出/缓存路径，禁止自行解锁或先执行 GN/编译再补报；现有模板与生效参数差异须保留。设置规则没有修改实际编译参数、构建图或缓存，没有执行 GN/Ninja。
- 此锁为项目代理操作规则，未设置操作系统权限或声称可阻止任意外部工具直接写文件。
- 按用户要求补充 Git 跟踪 PC 两套实际 `args.gn` 与展开源码 `common.gn`，配置模板/Android ARM64 参数及缓存路径清单原已跟踪；保存当前内容用于历史对比，未修改参数值或时间戳，未创建 Android 输出。其他 Chromium 源码/二进制/对象/缓存不纳入此次提交。

## 2026-10-04 项目整理与当前进度

统一入口为 [项目总览](../PROJECT-OVERVIEW.md)，已汇总项目目标、模块关系、目录职责、开发定位、剩余任务和构建保护规则；本文后续章节保留各阶段的实现及历史局部验证记录。

- 后续开发指引已补充到总览第 5 节：明确 SaaS Web、Go 控制面、指纹 Chromium 的职责，按需求判断修改归属，列出真实链路排障、桥契约联动及构建/发布规则；`AGENTS.md` 同步约束，项目记忆按用户要求记录同一边界。
- PC / Android 平台指引补充到总览第 5.4 节：明确共享业务/契约与各端宿主、源码/JNI/系统能力、构建输出和验证边界；当前 Windows x64 参数不直接用于 Android，移动 SaaS 样式也不触发 Android 内核修改。平台说明、开发规则和记忆同步，未进行 Chromium 编译或设备验收。

- 当前阶段：SaaS Web / Go 控制面主要功能及桌面、Android 原生能力已编码；最新 Chromium 链接、Android 完整构建、设备与多设备运行、生产发布验收尚未完成。
- 本次复测：网页 36 项回归、全部业务 JavaScript 语法检查、服务端 `go test -v ./...` 与 `go vet ./...` 通过；PostgreSQL 专项因未配置独立 `SAAS_TEST_DATABASE_URL` 跳过，不把历史数据库通过记录算成本次通过。Android/JVM 与原生局部检查本次未复跑。
- 现场修正：Development 的 `build.ninja` 现为 6,903,783 字节，包含实际构建图；旧记录“仅 558 字节”已过时。图存在不证明完整增量构建可用，Development 二进制仍为 2026-10-02，Release / publish 二进制为 2026-09-30，最新源码未重新链接。
- 构建配置核对：Development 为 `is_component_build=true`、关闭 ThinLTO、`is_debug=false`；Release 为静态组件、开启 ThinLTO，仅用于发版。`prepare_build_mode.ps1` 已改为内容哈希相同则不覆盖 `build/src/build-configs/common.gn` 和输出 `args.gn`，避免无实际配置变化时仅因时间戳触发 GN 重生成。
- PC/Android 构建隔离配置已纳入 Git：`build-configs/build-roots.json` 登记独立输出、临时目录、缓存和 D 盘 Android SDK/NDK/JDK/Gradle 路径；`build-configs/android-arm64.gn` 独立声明 Android ARM64 参数，不复用 Windows PC `common.gn`。只登记配置，未创建 Android 输出、未安装工具、未执行 GN 或编译。
- 整理：42 个历史页面快照/截图/日志所在 `.playwright-cli/` 移入 Windows 回收站；最终验证截图、原有效文档和全部 `build/`、`publish/` 及 Chromium 相关源码/缓存/编译产物保留。
- 打包：新增独立 SaaS 固定名称打包入口 `utils/package_saas.ps1`，真实编译服务并收录前端运行资源。项目内未发现既有 SaaS 包，首次默认 `output/saas/fingerprint-saas.zip`；外部已有包通过 `-OutputPath` 保持原名，后续覆盖同一路径，旧包送回收站。
- 下一步：在用户明确允许后，使用固定 D 盘工具链环境核对 Development 增量范围，再安排最新 PC 链接及白名单/隔离/Worker 联调；当前不启动 Chromium 编译。

## 当前阶段

本阶段完成了独立 SaaS Web 架构、桌面原生能力桥和 Android 契约的编码工作。SaaS 业务前端与后端位于独立项目中，Chromium 只负责受 Origin 白名单保护的本地能力，不承载登录、工作区、账号目录、同步和计费业务。

## 已完成

- 开发版和 Release 双编译配置：开发版启用组件化编译，Release 关闭组件化编译；Release 保留 ThinLTO，并关闭 Widevine。
- 现有受管理 Tab 的单父窗口模型、独立持久化 StoragePartition、账号级代理、User-Agent、UA-CH、硬件并发数和指纹种子。
- Cookie、LocalStorage、SessionStorage 的读取、写入、快照导入和导出链路。
- 独立 `saas-web/` 前端：真实登录、工作区、账号目录和 `window.saasBridge` 客户端封装；普通浏览器不使用演示数据或假接口。
- 独立 `saas-server/` 后端：用户会话、工作区、成员权限、账号目录、加密快照、版本冲突、租约、审计、静态 Web 托管和 API CORS 预检。
- 服务端可配置限流：鉴权接口按真实 `RemoteAddr` IP，其他 API 按已验签用户或 IP；中文 `429`、`Retry-After`、并发安全、总容量限制和请求驱动过期回收。五项 `SAAS_RATE_LIMIT_*` 参数及部署边界见 `saas-server/README.md`，默认启用、窗口 `1m`、鉴权 `30` 次、其他 API `300` 次、总容量 `10000` 条。
- 编译时完整 Origin 白名单：协议、主机和端口精确匹配，普通来源不能调用原生桥。
- 常驻 SaaS 启动规则：PC 自动打开固定宿主页，Android 原生 Tab 初始化完成后打开控制台；关闭单页和批量关闭均保留控制台，应用退出正常释放。
- PC 和 Android 共用 `chrome/common/chrome_switches.cc` 中的默认 SaaS 地址，后续改地址只需重编译该文件并重新链接对应平台；默认来源权限随常量更新，不改 GN。
- 桌面原生桥：Tab、存储、指纹、系统绝对路径及 `SaasFiles` 相对路径文件读写和自定义 HTTP 请求。
- 独立 Web 已接入成员邀请/接受、角色管理、账号授权和审计界面；同步冲突支持保留云端、合并与明确覆盖，覆盖仍执行具体版本校验。
- HTTP 页面加密兼容：无 WebCrypto 时调用受同一白名单保护的原生 PBKDF2/AES-GCM 接口；密码只存于当前页面内存，仍需运行版联调。
- Android 平台无关桥接契约，后续使用 Android Storage Access Framework、Keystore 和原生网络适配。
- Android 独立快照加密模块：`SnapshotCrypto`、`SnapshotJson` 和 Kotlin 适配器已实现真实 PBKDF2/AES-GCM，与网页信封互通；平台接口默认不获得页面授权，具体消息宿主挂接状态见下一项。
- 已补充 Android 消息端口宿主、精确来源策略、后台加密路由和 JNI 配置挂接源码；独立路由仅提供 `crypto`，成功绑定 TabModel 后增加 `tabs/storage/fingerprint/http`，宿主绑定 SAF 适配后增加 `files`，目录仍需系统授权。页面指纹覆盖不代表 Worker 一致性已完成；没有生成或安装新 APK，不能把已编码挂接当作 Android 浏览器已可运行。
- Keystore 通用 `secureStorage.get/set/remove` 已编码接入宿主，与来源和键名绑定的 AES-256-GCM 密文保存在应用私有存储；真实 AndroidKeyStore 持有设备密钥，不导出密钥、不承载 SaaS 业务。独立 Web 持久保存刷新令牌/设备标识/服务地址，恢复必须经服务端验证，访问令牌不持久保存；已保护页面重载、原生失联与失败不降级明文，退出标志和串行操作防止旧凭据复活。普通浏览器/桌面过渡端无此能力时保留原有 SessionStorage 行为。
- 后续单代理阶段已编码安卓固定账号分区工厂、按 BrowserContext 保存的本地配置、代理/指纹种子平台钩子、JNI 创建入口、禁止共享复制与 Java 单页导航保护。正常 Tab 的列表/创建/切换/导航/关闭已接入真实模型与创建器源码；绑定平台实现时声明 `tabs`，未绑定仍不可用，尚无完整 Android 编译或设备验收。
- 桌面 iframe 请求固定发往原生宿主来源并校验回复 Origin；HTTP 服务增加 `frame-ancestors`，避免第三方嵌入页面伪装原生桥。
- 构建模式、同步边界、桥接消息、部署方式和测试结果文档。

## 已完成的局部验证

- `saas-server`：`go test ./...`、`go vet ./...` 通过。
- 限流专项：超限与精确到期恢复、拒绝不延长窗口、伪造转发头、用户跨会话/IP 计数、无效令牌回退 IP、豁免请求、中文 `429` 和 `Retry-After` 取整、容量保留与过期回收均通过；512 协程的同标识额度及不同标识容量测试连续运行 20 次通过。整合后已配置独立测试 PostgreSQL，再次执行 `go test ./...` 和 `go vet ./...` 通过。竞态检测因缺少 CGO 所需 C 编译器未运行，配置包额外覆盖率采集被 Windows 拒绝执行，普通配置测试通过。
- Android 独立模块：JVM 55 项、网页与 JVM 双向互通 39 项、Kotlin 适配器 8 项通过，包含错误密码、跨账号、篡改、Unicode、14 MiB 明文及 16 MiB 密文边界；只证明模块和协议兼容，未完成 Android 设备、Chromium 桥或 APK 验证。
- 本轮 Android/消息复测共 461 项通过：加密 55、来源 136、消息路由 20、JVM 网络 187、网页加密互通 39、真实端口互通 16、Kotlin 8。网络模块需 JDK11+，未接入 Android；端口初始化事件为测试上下文注入，不构成 Android 主框架或生命周期的运行验收。
- 新增 JNI 生成、Android 源文件清单语法与源码补丁反向应用检查通过；未执行 GN 重生成。网页同步 9 项、iframe 消息安全 2 项及真实 PostgreSQL 全包复测通过。
- 单代理阶段：新账号环境头文件、共享 `chrome_content_browser_client.cc` 和参数测试源码局部语法检查通过；原生参数测试对象编译及链接通过，但运行遇到入口加载错误/系统拒绝启动，未计为运行测试通过，未修改系统策略。新增 JNI 和账号环境补丁反向检查通过；网页 11 项回归通过。
- 正常 Tab 接入阶段：JVM/协议复测 474 项通过，其中消息路由 33 项包含 13 项平台路由探针，只验证分发与拒绝边界，不模拟 Android 标签。三个平台 Java 文件解析检查、两个 JNI 生成、账号环境/状态 JSON 局部 C++ 检查与新补丁反向检查通过；网页 11 项通过。正常模型操作还未进行 Android 类型检查、完整链接或设备运行验收；当轮未提供 `storage/fingerprint/files/http`，后续存储进展见下一项。
- 后续存储阶段：`storage.getSnapshot/writeSnapshot` 已编码接入真实原生固定分区和隔离世界，绑定后提供 `storage`；未绑定仍不可用，完整指纹、SAF 文件和原生 HTTP 尚待接入。已修复 host-only Cookie 被恢复为域 Cookie，以及网页存储保留键 `__proto__` 丢失问题；桌面对应路径同步最小修复。
- 存储专项的 JVM/协议 489 项、实际 C++ 生成脚本的 23 项边界测试和网页 12 项通过；原生引擎/桌面局部语法检查、JNI 生成与 Java 源码解析通过。脚本存储对象为明确的测试探针，不替代真实平台；完整 Android 类型检查、CookieManager/renderer IPC 和设备读写尚未验收。没有 GN 重生成、全量编译或新 APK。
- 后续指纹阶段：安卓 `fingerprint.get/set`、创建配置、UA/UA-CH 及页面硬件覆盖已编码接入；普通冻结状态版本 2 保存 UA/硬件值并读取版本 1 的默认值。快照包含实际配置并在写入前核对；创建/导航显式选择 UA 覆盖，恢复先建空白环境，不自动重放页面 POST。Worker 一致性、实际请求头和平台效果仍未验收。
- 指纹专项：纯配置校验 37 项、原生状态头 59 项、JVM/协议 499 项及网页 13 项通过；元数据测试编译/链接成功，但入口加载错误导致运行未通过。环境/存储及共享浏览器文件局部 C++ 检查、JNI 生成、Java 解析和补丁反向检查通过。完整 Java 类型检查、Android 编译、设备效果及新 APK 尚未完成，未执行 GN 或全量编译。
- SAF 专项：真实目录树授权与按来源映射、相对路径逐级确认、文件读写、实际内容读回校验和保留备份覆盖已编码。文件策略 51 项、消息路由 71 项，JVM/协议共 563 项和网页 14 项通过；两个宿主文件仅通过 Java 语法解析，提供方关系/重命名与系统撤权尚未设备验证。仅新增 Android 源文件清单项，未执行 GN 或全量编译；备份和失败临时文件不永久删除。
- 原生 HTTP 专项：实际 SimpleURLLoader、独立内存/账号固定分区、显式凭据、同来源重定向、授权取消和有界响应已编码；原生参数运行 78 项、JVM/协议 575 项及网页 14 项通过，其中文件策略补充恢复文件后缀兼容后为 57 项。引擎局部 C++ 检查、JNI 生成、Java 解析通过。JVM loopback 187 项不是 Chromium 后端运行验证，真实网络、代理、Cookie 与取消效果仍待 APK/设备验收；无新增 GN 项或全量编译。
- Keystore 专项：JVM/协议 667 项通过，包含真实安全值加密 79 项、消息路由 90 项；网页 32 项通过，其中会话模块和实际 app.js 函数/提交处理 17 项覆盖异步竞争、缓存清理失败、设备绑定、用户切换、断网/401 和表单快照。两个宿主文件通过 Java 语法解析，补丁反向检查与 Android 源清单格式检查通过；Android 类型检查和系统 Keystore/磁盘效果未验收，没有 GN 重生成、全量编译或整体验收。
- Worker 后续编码：Dedicated/嵌套 Worker、SharedWorker 和 ServiceWorker 均按账号 StoragePartition 传递创建时硬件值快照，ServiceWorker 同时继承账号级 UA/UA-CH 覆盖，WorkerSettings 优先使用硬件快照；修改 UA 或硬件值返回重载提示。相关变更已保存为独立补丁并通过现有源码反向校验；Worker 实际网络/运行效果仍需验证，`worker_fingerprint_verified=false` 保持，未执行 GN 或全量构建。
- SaaS 界面：商业参考、任务/状态矩阵与 shadcn 视觉规范已建立，桌面/安卓初始空状态高保真稿已实际渲染并检查，位于 `docs/design/`。完整流程状态图及生产页面迭代未完成，不把静态设计稿当作可用业务页面。
- SaaS 工作台首轮落地：真实账号列表支持搜索、权限筛选、多选、批量同步/恢复和编辑资料，移动端导航改为底部四项操作栏，桌面/移动统一为浅色 shadcn 中性主题；Node 语法与 35 项网页回归通过。同步冲突、成员/安全页面状态和账号详情已完成首轮编码。
- 同步冲突交互已完成首轮：显示本地目录版本和云端当前版本，明确保留云端、合并后同步、明确覆盖三种后果与覆盖风险；版本再次变化时更新提示并要求重新确认。真实冲突接口逻辑未改变，Node 回归继续覆盖版本竞争。
- 成员与权限页面已完成首轮状态优化：根据真实工作区角色显示角色说明、只读/当前账号状态和空成员提示，邀请、角色修改、移除成员和账号授权继续使用真实接口。完整移动权限布局和设备安全页优化仍待推进。
- 设备与安全页面已完成首轮状态优化：显示当前设备、设备标识、有效期、撤销/退出动作，并根据真实 `secureStorage` 能力说明会话保护状态；服务端会话撤销接口不变。移动安全状态和完整设备审计仍待推进。
- 批量同步/恢复已增加真实进度条和逐账号失败结果，部分失败不会丢失已完成数量；继续使用现有租约、版本冲突和权限校验，Node 回归通过。
- 批量同步/恢复已区分服务端 `lease_conflict` 与 `lease_required`，页面分别提示其他设备占用和租约失效，避免把租约状态误报为普通失败。
- SaaS 工作台后续状态已补充：账号目录区分服务端加载中、读取失败、无数据和无筛选结果；账号详情显示真实目录资料，并在原生桥可用时读取对应账号的真实 Tab 运行状态，未连接时明确显示不可读取，不填充假状态。
- 批量租约提示支持同一批次多个账号分别保留占用/失效原因，成功、锁定、退出或切换工作区后清理旧提示；运行 Tab、设备会话、成员权限和批量进度补充 `aria-busy`、进度值与当前导航语义。
- 移动权限与设备安全细节已补充：账号授权面板显示查看者/编辑者的真实权限说明并区分授权读取失败；设备页显示当前会话设备标识、真实安全存储能力和会话加载状态，Tab/会话刷新期间禁止重复请求。
- 加密快照导入/导出已接入独立 Web：导出仅保存服务端加密信封，导入必须使用当前页面解锁密钥完成账号校验、解密校验、租约获取和条件版本写入；只读账号不能导入，失败或版本冲突不会报告成功。
- 账号目录删除已接入真实服务端：仅工作区所有者/管理员可见，删除前要求明确确认；服务端拒绝活跃租约账号，数据库级联清理快照、授权和租约，并保留 `account_deleted` 审计记录。
- 代理快照字段已完成首轮拆分：兼容保留 `proxy_rules` 历史格式，同时增加 `proxy_config.address`、`auth_ref` 和凭证结构校验；历史明文代理配置不会被导入校验拒绝，凭证的实际 Keystore/认证引用绑定仍随平台代理实现验收。
- 浏览器壳改造首轮已接入 SaaS 账号打开幂等控制：同一账号存在并发打开请求时复用同一请求，创建后重新读取并激活唯一账号 Tab；固定 SaaS 首页和父窗口约束继续由现有 Chromium 补丁提供。
- SaaS 账号表已接入真实原生 Tab 状态：按当前工作区过滤 `tabs.list()`，已有账号显示“运行中/切换 Tab”，未运行显示“打开环境”，原生桥不可用时明确显示“未连接”；不写入演示 Tab 状态。
- SaaS 页面已增加真实运行环境 Tab 条：第一项为不可关闭的 SaaS 控制台，后续账号 Tab 来自原生列表；点击切换，关闭按钮只关闭对应账号环境，标题按可用宽度省略，未连接或读取失败均显示明确状态。
- Chromium 桌面原生 Tab 壳沿用上游 `TabStripLayout` 的可用宽度分配：Tab 在可用宽度不足时连续压缩到最小宽度，TabStripRegionView 不引入横向滚动容器；新增 SaaS 壳验收以确认固定控制台与账号 Tab 不破坏该布局。
- 移动端编辑、账号授权、同步冲突和邀请弹窗已改为全屏面板布局，避免窄屏中多列操作拥挤；实际权限和接口逻辑不变，移动端设备运行仍待后续验收。
- Chromium 内建用户入口：工具栏头像按钮的最终可见性已在 Windows/Android 产品模式锁定为隐藏，命令行显示参数不能覆盖；局部 C++ 检查、补丁反向检查通过。新版 Chrome 尚未重新链接，运行版和 Android 构建仍待后续验证。
- `saas-web`：Node JavaScript 语法检查通过；会话、桥接、加密同步和页面状态契约共 36 项网页回归通过。
- Chromium WebUI：TypeScript 静态检查通过。
- 独立 Web 加密同步：真实 PBKDF2/AES-GCM 往返、账号绑定、错误密码拒绝、同步类别过滤及旧 WebUI 信封兼容测试通过。
- 合并与冲突：Cookie 分区/域身份、网页存储同键合并、不同来源拒绝合并、覆盖遇到新版本继续拒绝等测试通过。
- 网页同步共 9 项测试通过，包含获取租约期间锁定、恢复读取 Tab 期间切换会话后停止写入。退出/切换工作区清理旧 Tab 与设备操作、密码及待处理冲突；邀请弹窗使用操作代次防止旧响应污染重新打开的表单。
- 权限：真实 PostgreSQL 验证账号只读不能被工作区编辑权限绕过、工作区只读不能被账号授权提升、邀请只能使用一次及成员移除后失权。
- 独立网页真实页面验证：登录、成员列表、邀请创建/接受、受邀成员登录、账号权限弹窗和审计记录读取通过；编辑者的邀请按钮禁用，邀请弹窗关闭后清理令牌。本次使用隔离 PostgreSQL 与 Edge 测试会话，不替代 Chromium 原生桥运行验收。
- 文件：直接复用实际原生辅助函数和现有基础库，在 Windows 实测绝对/相对路径、中文文件名、二进制、空文件和失败时保留原文件；未宣称已经完成浏览器 JS 桥接联调。
- 真实 PostgreSQL 集成：隔离测试实例下的初始化、登录、跨设备快照读取、并发租约、版本冲突、覆盖审计和会话撤销测试通过。
- 常驻控制台：六个桌面 C++ 源文件局部语法编译、TypeScript 检查、Android JNI 生成和新增补丁反向应用检查通过。
- 默认地址常量所在 `chrome_switches.cc` 已使用现有 Development 参数完成单文件对象编译，未调用 GN；还需要重链接后才能更新运行版。
- 原生桥：`management_ui_handler.cc` 使用现有开发版编译参数完成 C++ 局部语法编译。现有生成头尚无新的 Origin 白名单标记，局部检查显式补充与 `common.gn` 一致的宏定义，不修改生成文件或 GN；最终构建仍需生成正式头并链接。
- 新增 SaaS 桥接补丁可通过反向应用检查，并已加入 `patches/series`。
- 指纹配置编辑表单已在现有指纹管理器 WebUI 中实现，包含 User-Agent、硬件并发数和指纹种子输入；尚未在包含最新源码的运行版中回归。
- 本轮曾执行 Development GN 重生成并启动过 Ninja 依赖验证，随后按用户要求停止；未完成链接、未生成新 Chromium 二进制，也未执行 Release 全量编译。

## 尚未完成

- 最新源码尚未重新链接进入可展示 SaaS 页面的新开发版二进制；当前测试版仍是旧的 Development 输出。
- 独立 SaaS Web 在 Chromium 开发版中的白名单桥接、文件和 HTTP 运行联调。
- `window.open`、`target=_blank`、页面跳转和会话恢复的单 Tab 运行回归。
- HTTPS 反向代理及客户端多设备同步、冲突恢复运行测试；服务端 PostgreSQL 集成验证已通过。
- IndexedDB、Cache Storage、Service Worker 的同步范围和一致性实现。
- Android 独立 SDK/NDK 构建输出、已编码消息宿主和 JNI 的完整 Java/C++ 编译、Worker 指纹一致性、Keystore/原生 HTTP/SAF 的平台权限与隔离验证、APK/AAB 和设备回归；已完成的 JVM 与端口互通不代表这些平台项完成，整体验收按用户要求暂缓。
- PC 新 Chrome 尚未链接；后续核对 Release 缓存与依赖并统一构建，Development 仅作为历史构建状态保留。单文件编译、局部语法检查和干跑不能作为新运行版验收。
- Release 全量编译、发布目录、安装升级和整体回归测试。
- SaaS 首轮商业页面编码、响应式布局、搜索/筛选/选择/批量操作和关键加载/失败/只读状态已完成；剩余为高保真图扩展、运行版桥接、多设备联调和发布验收，不计入本轮编码缺口。

## 编译与变更规则

- 日常修改优先局部静态检查或目标编译，不主动执行 GN 重生成。
- 预计引发大范围重新编译的 GN 或公共配置变更，先说明影响并取得许可。
- 所有功能编码完成后，统一进行必要的 Release 批量编译和整体测试。
- `build/src` 为本地 Chromium 源码输出目录；可持久化的 Chromium 修改必须通过 `patches/series` 重放。

## 已知独立问题

`disable-gcm.patch` 已修正 1 处 hunk 行数，Git 格式解析通过；`build-compatibility.patch` 已修正 15 处行数，补丁读取检查通过，但仍有 GPU 和 Blink 两段纯上下文块使 Git 完整格式解析失败。修正只涉及 hunk 计数，没有修改正文或 GN。历史核对确认 Blink 段为撤销依赖后遗留的上下文，GPU 段首次提交即缺少增删标记，暂未猜测修复或删除；从净源码完整重放仍待验证。

此前 Development 前端局部目标在 Ninja 自动重生成时因工具链发现失败而停止。已核实 Visual Studio 2022 和 Windows SDK 10.0.26100.0 分别安装在 D 盘 Visual Studio 目录与 `D:\Windows Kits\10`；它们并未缺失。局部验证必须复用 `DEPOT_TOOLS_WIN_TOOLCHAIN=0`、D 盘 Visual Studio/SDK、D 盘临时目录和 `PYTHONUTF8=1`，避免自动下载工具链或因系统代码页读取环境文件失败。

此前核对 `build/src/out/Development/build.ninja` 仅 558 字节，只包含 GN 重生成入口；2026-10-04 现场核对已为 6,903,783 字节并含完整构建图，此项旧阻塞已变化。此前临时 driver 对 `chrome.dll` 的干跑列出 54340 个待执行步骤，这些步骤没有执行，PC 新 Chrome 没有完成链接。当前仍需核对缓存/依赖并安排统一构建，不能仅据图文件存在宣称构建已通过，也不能归因为 Windows SDK 缺失或将干跑步骤数记为已完成编译。限流任务的 CGO 编译器缺口属于 Go 竞态检测环境，与上述 Chromium 构建状态不同。

Android 环境另有独立限制：WSL 返回 `HCS_E_HYPERV_NOT_INSTALLED`，现有 Linux 发行版不能启动。本轮没有擅自修改系统功能或重启；先完成可独立验证的编码和测试。生产地址仍需设为设备可访问的实际 SaaS 服务，不能把 Android 的本机回环地址当成 PC 服务地址。

## 下一步顺序

2026-10-03 调整：用户将异常关闭恢复降为低优先级，相关扩展暂停，先推进正常 Tab 管理、存储读写、指纹与独立 SaaS 联调。保留本轮已编码的状态头、固定分区恢复、普通关闭历史过滤和弹窗拒绝保护，不回退已有隔离约束。

本轮进一步调整：用户要求继续编码、暂不整体验收。优先推进剩余源码与局部验证，不恢复大规模构建、不重链接 Chrome、不启动 APK/设备联调；下列整体构建和运行步骤仅保留为后续待办，不在本轮执行。

此前状态头原生运行测试 51 项通过；账号参数测试重新编译和链接成功，运行仍遇到入口加载错误，未计为通过。JVM/消息/加密整合 461 项及网页 11 项当轮复测通过。现有源码部分的新增补丁反向检查与 JNI 生成通过；本地缺少的 `TabImpl.java` 调用保护已按真实上游 `144.0.7559.132` 核对补丁上下文，尚未应用到本地文件或完成 Android 整体编译，不能视为完整恢复链或 APK 验收通过。

1. 核对 Release 构建图和保留缓存，评估实际编译范围后统一构建并重新链接，使最新 SaaS WebUI 和原生桥进入可运行二进制。
2. 运行白名单页面、普通页面、双 Tab 存储隔离、文件读写和 HTTP 桥接测试。
3. 完成单 Tab 跳转、快照恢复和 SaaS 服务端真实数据库集成验证。
4. 完成 Android 工具链和平台适配后，再做移动端构建。
5. 所有功能验收后执行 Release 全量编译、打包和整体回归。
