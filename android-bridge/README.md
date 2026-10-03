# Android 原生桥接适配层

这里保存与 `saas-web/bridge-contract.d.ts` 对齐的 Android 契约和适配边界。当前阶段不复制 Chromium 桌面输出，也不伪造 Android 编译结果。

## 当前状态

- `SaasBridgeContract.kt`：平台无关的 Kotlin 接口和数据结构，文件与 HTTP 字段和桌面 Web 契约保持对应。
- `SnapshotCrypto.java`、`SnapshotJson.java` 与 `JvmSnapshotCryptoAdapter.kt`：可独立编译的真实快照加密模块，使用 Java 标准 JCA/JCE，无新增运行时依赖。
- `SaasBridgeHost.java` 已编码接入 Chromium 的真实主框架消息端口，`SaasBridgeDispatcher.java` 路由真实加密及已绑定的平台标签/存储/页面指纹/原生 HTTP；宿主绑定真实 SAF 和 Keystore 适配器后增加 `files/secureStorage=true`，目录访问仍需系统授权。独立路由未绑定平台时仅声明 `crypto=true`；尚未完成完整 Android Java/C++ 编译和设备验收，不能将源码挂接当作已安装可用的 Android 浏览器。
- `SaasOriginPolicy.java` 使用可信宿主提供的完整 Origin 白名单，默认 URL 不在模块内硬编码；Android JNI 从现有 C++ 常量和编译参数读取配置，默认来源随该常量更新。
- 文件能力必须使用 Storage Access Framework 返回的授权 URI；禁止把 Windows 路径模型带入 Android。
- 会话密钥使用 Android Keystore；应用生命周期恢复必须覆盖进程被系统回收、断网和权限撤销。

## 接入顺序

1. 完成已编码的 Android Chromium 消息宿主、Origin 校验及 JNI 的完整编译和设备验收。
2. 接入 `tabs`、`storage`、`fingerprint` 三项已稳定桌面契约。
3. 增加 Keystore 会话、应用恢复和账号目录隔离测试。
4. 最后实现 SAF 文件能力和可取消的原生 HTTP，并执行权限撤销与审计测试。

## 快照加密模块

运行条件：Android API 26+ 或支持 AES-256-GCM 的 JVM 8+（已用 D 盘 JDK 17 编译为 Java 8 字节码）。模块使用 `java.util.Base64`、`GCMParameterSpec` 和标准 JCA/JCE；未执行 Android 设备或 Chromium 集成验证。

- PBKDF2-HMAC-SHA-256 默认 600000 次，支持 600000～2000000 次；派生 32 字节 AES 密钥。直接以 UTF-8 密码字节执行标准 PBKDF2，避免各 Android PBEKeySpec 提供者对 Unicode 的差异。
- AES-256-GCM 每次生成新 16 字节盐和 12 字节 nonce，128 位标签单独输出。解密接受 16～64 字节盐。
- AAD 严格为 UTF-8 `fingerprint-manager:v1:` 加原始 `accountId`，不修剪或规范化账号、密码；UTF-8 未配对代理项替换行为与网页 TextEncoder 一致。
- 信封字段为 `algorithm`、`kdf`、`iterations`、`salt`、`nonce`、`ciphertext`、`tag`，后二者分开，二进制字段使用标准 Base64；兼容网页 atob 的省略填充和有界 ASCII 空白。
- 加密明文上限 14 MiB、解密密文上限 16 MiB；信封二进制字段总字符数上限 22369880，单项 Base64 在解码前检查长度。密码为 12～4096 个 UTF-16 代码单元，非空账号最多 1024 个代码单元；JSON 最多嵌套 64 层、200000 个值，拒绝循环、重复键、非有限数和非法 UTF-8。
- 序列化支持字符串键的 Map、List、null、String、Boolean 以及 Java 基本数字包装类型；快照版本、账号、Cookie、网页存储、同步选项、页面来源和指纹按网页契约校验。页面地址要求符合 URI 语法；如上层接收非规范页面 URL，应先经 Chromium URL 解析器规范化。
- 不缓存、不持久化密码与密钥，不记录敏感内容；清理本模块可控的密码字节、字符数组、派生密钥及明文字节。JVM/Android 的不可变 String、序列化临时对象及 JCA 内部副本无法保证内存擦除；调用方也应及时释放请求对象。Java API 的调用方拥有传入的 `char[]`，使用后自行清零。快照密码不写入 Keystore；此前会话密钥规则不适用于快照密码。
- 错误只携带中文消息，不附底层异常；错误密码、账号 AAD 不匹配和认证失败统一拒绝，认证成功后仍验证明文账号，避免同一 AAD 字节表示下的账号混用。

## 契约接入方法与未接入边界

`SaasBridgeContract.Crypto` 提供与网页一致的两个方法，使用 `SnapshotEncryptOptions`、`SnapshotDecryptOptions` 和 `SaasSnapshotEnvelope`。现有实现无需修改，可空 `crypto` 属性默认返回 null；独立 Kotlin 适配器不会自动获得页面授权。新 Chromium 宿主通过真实消息路由调用 Java 加密引擎，只有在可信主框架成功绑定端口时才提供能力查询。

新消息宿主已编码校验真实来源、请求类型及消息大小，路由 `crypto.encryptSnapshot` 与 `crypto.decryptSnapshot`，拒绝非整数或越界 `iterations`；使用有界后台执行器，不在 Android 主线程派生密钥。Kotlin 适配器的 `suspend` 方法本身不切换线程。页面登录和工作区权限仍由独立 SaaS 校验，不把业务逻辑复制进 Chromium。

本模块已接入原生 Tab/存储、SAF 文件和 Keystore 通用安全存储源码，但尚未完成平台验收；完整 SDK/NDK 构建和 APK 尚未提供。消息宿主已有导航、WebContents 替换、渲染进程退出和销毁时撤销逻辑，但这些 Android 平台生命周期行为尚未经过设备验证。按本轮用户要求继续编码，整体验收暂缓，不启动整体构建或设备联调。

## 独立验证

测试源位于 `src/test/` 和 `tests/`，只用于明确的自测场景；执行真实加密算法和网页模块，不包含替代业务的 mock。密码和明文互操作数据通过内存及标准输入输出管道传递，不写入文件、不放入命令行参数。编译器、临时目录与编译输出均放 D 盘。

```powershell
pwsh -File android-bridge/tests/run-jvm-tests.ps1
# 如已有 D 盘 Kotlin 编译器，增加此参数可同时编译契约、适配器及 Kotlin 自测。
pwsh -File android-bridge/tests/run-jvm-tests.ps1 -KotlinCompilerDirectory D:\codex-tools\kotlin-2.2.20\kotlinc
```

脚本默认复用 `D:\Program Files\Eclipse Adoptium\jdk-17.0.18.8-hotspot` 和 `D:\Program Files\nodejs\node.exe`，可用参数覆盖；输出默认创建于 `D:\tmp\android-bridge-crypto-<随机标识>`。不指定 Kotlin 目录时会明确报告未验证 Kotlin，而不是伪造编译结果。

覆盖 PBKDF2 标准向量、每次随机盐和 nonce、中文/emoji/组合字符/NUL/未配对代理项、空参数与长度限制、错误密码、跨账号、篡改、非法 Base64/JSON/UTF-8、盐与派生上限，以及直接调用 `saas-web/snapshot-sync.js` 的网页↔JVM 双向互操作。

本次验证使用 D 盘 JDK 17、Node 和 Kotlin 2.2.20：JVM 55 项、网页↔JVM 39 项、Kotlin 适配器 8 项全部通过，包含 14 MiB 明文、16 MiB 密文精确边界和 Unicode 账号 AAD 碰撞后的账号校验。仅证明独立模块与网页协议兼容，不代表 Android 设备或 Chromium 消息通道验证完成。

## 原生端口宿主与本轮验证

`src/chromium/java/` 保存真实 Chromium 宿主源码；持久化挂接位于 `patches/upstream-fixes/managed-tab-android-native-port.patch`，包含所需 Java 源码、仅安卓使用的源文件清单和现有 TabModel 的 JNI 挂接。核心模块只提供原生能力，不保存 SaaS 登录、工作区或业务状态。

宿主读取真实 `RenderFrameHost` Origin；端口只交给白名单主框架。初始化为 JSON 字符串，网页不能自报来源申请授权；空附带端口数组为正常请求，过期端口回调不能撤销新连接。导航撤销后在运行任务继续计费，只有真实完成或取消等待任务才释放预算。JSON 包装超限返回明确错误，不返回截断数据；字节预算在完整 UTF-8 分配前校验。

整合复测：来源策略 136 项、消息路由 20 项、真实 MessageChannel／网页／JVM 互操作 16 项通过，原有加密与 Kotlin 测试继续通过。端口初始化事件只在隔离测试上下文注入，消息端口和加密路由是真实实现；不等同于 Android 设备测试。新增 JNI 已生成验证，源码补丁反向应用及安卓源文件清单语法检查通过，未执行 GN 重生成或整体构建。

## JVM 网络辅助模块的边界

`SaasHttpClient.java` 编译为 Java 8 字节码，但真实网络后端需要 JDK11+ 的独立 HttpClient，使用公开反射调用，不读取或修改全局 CookieHandler/Authenticator。真实 loopback 回归 187 项通过，覆盖全局 Cookie 并发变化、显式凭据、重定向、取消、响应限制与控制字符拒绝。

该模块不是 Android 原生 HTTP 的实现，未打包进 Chromium 安卓源文件清单；新安卓网络挂接使用另行编码的 Chromium 网络层，见网络专项，不调用该 JDK 辅助类。缺少 JDK11 后端的运行时明确拒绝请求；JVM 辅助模块仍仅支持列明的七种方法及受限类型化头，不自动携带浏览器 Cookie，`includeCredentials=true` 明确拒绝；不能据 JVM 测试宣称安卓网络已运行验收。

## 安卓账号环境创建基础

`src/chromium/native/saas_account_environment.h` 与 `managed-tab-android-account-environment.patch` 提供创建基础。账号使用 `saasandroid` 域下的固定持久化 `StoragePartition`，先设置代理/指纹种子，再创建 SiteInstance 和 WebContents；不通过清空全局 Cookie 或无痕模式模拟隔离。配置注册表绑定真实 BrowserContext，活跃账号不能重复创建，不同配置不能静默复用已有网络缓存。

现阶段关闭环境后，如代理或指纹种子与已有缓存不同，需要重启浏览器，不报告虚假的配置切换成功。账号环境不允许 Clone 复用同一分区；Java 新 Tab 导航入口复用原页面，保留请求正文、发起来源和额外请求头。另一个已创建 Popup 的接收入口拒绝第二个账号页面，不把缺少 POST 正文的 URL 假装转换成原页导航；完整单页跳转仍需设备回归。

正常使用链路已在 `managed-tab-android-tab-operations.patch` 接入 `tabs.list/create/activate/navigate/close`。使用真实 TabModel、TabCreator 和 TabRemover，在 UI 线程重新校验消息端口、文档代次、真实主框架 Origin 与等待期限；控制台和普通标签不能作为账号目标，冻结/归档环境参与重复账号检查。创建先建立固定分区并核对 WebContents 所有者，再加载网页；创建失败清理实际所有者，不退回普通标签。

平台实现成功绑定时提供 `tabs/storage/fingerprint/http/crypto`；宿主同时绑定 SAF 和 Keystore 适配器后提供 `files/secureStorage`，但并不预授予目录访问权限。未绑定的独立路由仍只提供 `crypto`。指纹覆盖限于当前页面配置，Worker 一致性待完成；普通冻结读取使用必需状态头和真实分区元数据，异常关闭自动恢复按低优先级暂缓。

本轮独立复测 474 项通过，包含 33 项消息路由测试；其中 13 项仅为分发边界探针，不模拟 Android 标签。三个平台 Java 文件解析、两个 JNI 生成、环境/状态 JSON 局部 C++ 检查和新补丁反向检查通过。未完成 Android 类型检查、完整编译或设备运行；原生参数测试对象/链接成功但加载入口错误仍未解决，不计为运行通过。

验证边界：实际原生参数函数测试源码保存在 `tests/account-environment-validation.cc`，可复用 `utils/check_cpp_syntax.py` 的现有编译参数。头文件、共享浏览器代码和测试源码语法检查通过；测试对象编译和链接通过，但测试程序启动被系统拒绝，不能宣称参数运行测试或 Android 分区运行验收通过。新增 JNI 生成和源码补丁反向检查通过；没有全量构建或修改系统策略。

## 完整构建限制

### Keystore 安全存储编码进展

`SaasSecureStorage.java` 提供按真实 Origin 隔离的 `secureStorage.get/set/remove`，只识别键名和不透明文本，不识别登录、令牌、工作区或其他 SaaS 业务。AES-256 密钥由真实 AndroidKeyStore 生成和持有，不提供导出接口；`SaasSecureValue.java` 使用随机 IV 的 AES-GCM，认证数据绑定版本、Origin 和键名，密文持久化到应用私有 SharedPreferences。密文和索引不保存来源/值明文，单值 UTF-8 上限 16 KiB，来源最多 16 条、应用最多 256 条。

全应用单后台线程与 4 项等待队列，串行创建密钥和提交存储；不在 UI 线程执行 Keystore 密码操作。导航代次、销毁和 15 秒宿主期限持续撤权，取消后的后续步骤不执行，不能撤回已经发出的系统操作；`remove` 只清除当前来源的逻辑条目，不删除文件或共享密钥。损坏密文、设备密钥失效和容量超限返回中文错误，不降级明文。是否具有硬件保护由真实设备决定；同键旧密文重放与服务端会话有效性不由存储层决定。

独立 Web 的 `session-persistence.js` 保存刷新令牌、认证时捕获的设备标识和刷新服务地址；不保存访问令牌、用户信息或快照密码。原生能力启用后清除旧 SessionStorage 会话，保护模式标记令后续页面在端口尚未重连时也不写回明文。退出标志不含秘密，原生清除失败时仍禁止自动恢复；串行保存/清除与操作代次防止旧写入、旧读取和旧用户刷新请求恢复已退出的会话。恢复必须经服务端刷新验证，断网保留凭据，401 清除当前会话。未具备安全存储的普通浏览器和桌面过渡端保留原有 SessionStorage 行为，不能称为 Keystore 保护。

本轮 JVM/协议 667 项通过（真实安全值加密 79、消息路由 90），网页 32 项通过，其中 17 项验证会话模块、实际 app.js 会话函数和登录表单快照；平台边界探针只存在于测试。两个宿主 Java 文件仅通过语法解析，Android 类型检查、系统 Keystore、SharedPreferences 磁盘提交和平台效果未验收。补丁保存到 `managed-tab-android-secure-storage.patch`，仅新增两个 Android 源清单项并检查格式，不执行 GN 重生成；没有整体验收或全量编译。

### 原生 HTTP 编码进展

`saas_http_request_config.h`、`saas_native_http.h` 与 TabModel JNI 接入 Chromium 的真实 SimpleURLLoader。无 `tabId` 时使用独立内存网络分区，禁用磁盘 HTTP 缓存，不访问共享 Profile Cookie；指定 `tabId` 时使用真实账号固定分区，可复用账号代理和已配置 UA（显式 User-Agent 优先）。只有 `includeCredentials=true` 且指定真实账号时才自动携带该分区凭据，不能与显式 Cookie 头混用；默认可以显式提供 Cookie/Authorization，不读取全局 JVM CookieHandler。

仅接收无用户名密码的 HTTP/HTTPS URL，支持 PATCH 等有效方法，拒绝 CONNECT，GET/HEAD 不允许指定上传正文，TRACE 不允许非空正文。传输长度、连接和代理认证头由网络层管理，拒绝控制字符与大小写重复头；不关闭 TLS 验证、不弹出网页 HTTP 认证窗口。可绕过网页 CORS，但不能绕过系统网络权限或服务端认证。

请求正文 8 MiB、响应正文 10 MiB，以适配现有 14/16 MiB JNI JSON 边界；请求/响应头最多 100 项、32 KiB，保留重复响应头到可选 `headersList`。同来源重定向最多 5 次，跨来源及协议降级拒绝；HTTP 错误状态保留真实状态和正文。每 Profile 最多 4 个请求，30 秒总等待、100 毫秒授权检查，宿主等待 35 秒；控制台撤权、账号跨文档导航和关闭取消后续网络操作，不承诺撤回已发送的数据或服务端副作用。

网络参数原生运行测试 78 项、JVM/协议 575 项、网页 14 项通过；包含文件策略 57 项，补充临时/备份文件保留扩展名，避免系统提供方自动补后缀导致正常覆盖失败。实际引擎头文件局部 C++ 检查、JNI 生成及 Java 语法解析通过。既有 JVM loopback 187 项不是该 Chromium 后端的网络运行测试；完整 Android 类型检查、网络请求、代理、Cookie 和取消效果仍待 APK/设备验证。没有新增 GN 项、重生成 GN 或全量编译。

### 授权目录文件进展

`SaasSafFiles.java` 接入真实系统目录选择器、ContentResolver 和 DocumentsContract；按真实页面 Origin 独立记录树 URI，必须持有相应的持久读写授权。网页只提交树内相对路径，不能提交绝对路径或任意 URI；每级子项都由提供方确认父子关系，不能确认或出现同名歧义时拒绝访问。

`files.list/read/write` 已编码，单文件上限 16 MiB，目录最多 1000 项，未知大小为 -1。首次调用没有有效目录授权时打开系统选择器；宿主等待最多 120 秒，网页等待 150 秒。单后台线程、两个等待任务，后台授权检查不访问界面对象；导航代次、超时、应用销毁和系统持久授权撤销会停止后续操作，已经发出的系统调用不能强制撤回。

覆盖时先创建 `.fingerprint-pending-` 独立文件，写完并读取校验真实内容，再将原文件改名为 `.fingerprint-backup-`，最后恢复目标名称；返回可选 `backupPath`，不删除原始内容、备份或失败的临时文件。提供方不支持重命名时明确拒绝安全覆盖。失败时尽量恢复原名称，无法确认时返回中文错误并保留文件；该流程不是跨提供方原子事务，用户可通过授权目录自行恢复。备份/临时保留名称不能通过写入 API 覆盖。

专项复测：文件策略 51 项、消息路由 71 项，JVM/协议共 563 项通过；网页 14 项通过。两个宿主 Java 文件通过语法解析，不代表 Android 类型检查或 SAF 提供方实际行为通过；Android 目录选择、权限撤销、重命名失败、跨来源映射和恢复仍需设备验收。仅修改安卓源文件清单并检查格式，不执行 GN 重生成或 Windows 全量编译。

### 存储专项进展

`src/chromium/native/saas_account_storage.h` 和 `saas_web_storage_scripts.h` 与 `managed-tab-android-storage.patch` 保存真实存储实现及挂接。Cookie 使用实际账号固定分区的 CookieManager，不访问 Profile 默认分区；网页存储只在 Chrome 内部隔离世界读取/写入，跨文档和来源变化时停止。Cookie-only 不需要访问 LocalStorage 或加载网站；快照读写尊重选中类别。

异步 JNI 不阻塞 UI，持续核对控制台授权；操作 30 秒超时、宿主等待 35 秒，同账号串行，Profile 内最多 8 个未释放操作。网页写入失败尝试回滚原数据；后续 Cookie 写入失败可能已产生局部修改，不报告原子成功。指纹快照核对已应用的配置，不能据存储接入宣称 Worker 或整机指纹效果完成。

### 指纹专项进展

`saas_fingerprint_config.h` 与 `saas_user_agent_metadata.h` 提供真实参数校验和数据驱动的 UA-CH 构建；`managed-tab-android-fingerprint.patch` 挂接 get/set、创建配置、控制台导航和普通冻结保存。只修改本地能力层，不承载 SaaS 业务。空 UA/硬件 0 清除相应覆盖，字段校验失败不提交半份配置；同步忙时拒绝修改。

纯配置测试 37 项、原生状态头测试 59 项、JVM/协议 499 项、网页 13 项通过；UA 元数据测试编译和链接成功但运行遇到组件入口加载错误，未计通过。共享浏览器/环境/存储局部 C++ 检查、JNI 生成和 Java 解析通过。页面 UA、HTTP 请求头、Worker 一致性和 APK 仍待真实运行验收；未修改 Blink、GN 或启动全量构建。

JVM/协议复测 489 项通过；`tests/storage-script-source.cc` 编译和链接实际原生脚本生成器，`tests/storage-script-selftest.cjs` 使用明确的存储探针验证 23 项边界，包括保留键、类别过滤、文档标记、配额失败和回滚；不模拟原生 CookieManager 或 Android 设备。网页 12 项、JNI 生成、Java 解析、原生引擎和桌面修复局部 C++ 检查通过；尚无 Android 类型检查、完整 IPC 或设备验收。

现有 WSL 的 Linux 发行版因虚拟化组件未启用而无法启动，尚无可运行的 Android Chromium 构建环境。本轮没有修改 Windows 系统功能、启用虚拟化或重启，也没有安装新工具。生产默认地址仍需由唯一 C++ 常量设置为设备可访问的真实 SaaS 服务；Android 的 `127.0.0.1` 指向设备自身，不会自动访问 PC 服务。
