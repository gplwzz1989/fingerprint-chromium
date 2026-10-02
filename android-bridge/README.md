# Android 原生桥接适配层

这里保存与 `saas-web/bridge-contract.d.ts` 对齐的 Android 契约和适配边界。当前阶段不复制 Chromium 桌面输出，也不伪造 Android 编译结果。

## 当前状态

- `SaasBridgeContract.kt`：平台无关的 Kotlin 接口和数据结构，文件与 HTTP 字段和桌面 Web 契约保持对应。
- `SnapshotCrypto.java`、`SnapshotJson.java` 与 `JvmSnapshotCryptoAdapter.kt`：可独立编译的真实快照加密模块，使用 Java 标准 JCA/JCE，无新增运行时依赖。
- `SaasBridgeHost.java` 已编码接入 Chromium 的真实主框架消息端口，`SaasBridgeDispatcher.java` 路由真实加密；尚未完成完整 Android Java/C++ 编译和设备验收。宿主成功建立端口后仅声明 `crypto=true`，其余能力为 `false`；未绑定的平台仍保持默认不可用，不能将源码挂接当作已安装可用的 Android 浏览器。
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

本模块尚未提供原生 Tab/存储、SAF 文件、Keystore 会话、完整 SDK/NDK 构建或 APK。消息宿主已有导航、WebContents 替换、渲染进程退出和销毁时撤销逻辑，但这些 Android 平台生命周期行为尚未经过设备验证。

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

该模块不是 Android 原生 HTTP 的完成实现，未打包进本轮 Chromium 安卓源文件清单，宿主 `http=false`。缺少 JDK11 后端的运行时明确拒绝请求；仍需接入 Chromium/Android 原生网络。当前仅支持列明的七种方法及受限类型化头，不自动携带浏览器 Cookie，`includeCredentials=true` 明确拒绝；不能据此宣称安卓已具备完整自定义网络能力。

## 安卓账号环境创建基础

`src/chromium/native/saas_account_environment.h` 与 `managed-tab-android-account-environment.patch` 提供创建基础。账号使用 `saasandroid` 域下的固定持久化 `StoragePartition`，先设置代理/指纹种子，再创建 SiteInstance 和 WebContents；不通过清空全局 Cookie 或无痕模式模拟隔离。配置注册表绑定真实 BrowserContext，活跃账号不能重复创建，不同配置不能静默复用已有网络缓存。

现阶段关闭环境后，如代理或指纹种子与已有缓存不同，会明确要求重启浏览器，不报告虚假的配置切换成功。账号环境不允许 Clone 复用同一分区；Java 新 Tab 导航入口复用原页面，保留请求正文、发起来源和额外请求头。尚未覆盖所有原生 Popup WebContents 入口，也未完成冻结和重启恢复，故不开放 `tabs` 能力。

验证边界：实际原生参数函数测试源码保存在 `tests/account-environment-validation.cc`，可复用 `utils/check_cpp_syntax.py` 的现有编译参数。头文件、共享浏览器代码和测试源码语法检查通过；测试对象编译和链接通过，但测试程序启动被系统拒绝，不能宣称参数运行测试或 Android 分区运行验收通过。新增 JNI 生成和源码补丁反向检查通过；没有全量构建或修改系统策略。

## 完整构建限制

现有 WSL 的 Linux 发行版因虚拟化组件未启用而无法启动，尚无可运行的 Android Chromium 构建环境。本轮没有修改 Windows 系统功能、启用虚拟化或重启，也没有安装新工具。生产默认地址仍需由唯一 C++ 常量设置为设备可访问的真实 SaaS 服务；Android 的 `127.0.0.1` 指向设备自身，不会自动访问 PC 服务。
