# Android 原生桥接适配层

这里保存与 `saas-web/bridge-contract.d.ts` 对齐的 Android 契约和适配边界。当前阶段不复制 Chromium 桌面输出，也不伪造 Android 编译结果。

## 当前状态

- `SaasBridgeContract.kt`：平台无关的 Kotlin 接口和数据结构，文件与 HTTP 字段和桌面 Web 契约保持对应。
- `SnapshotCrypto.java`、`SnapshotJson.java` 与 `JvmSnapshotCryptoAdapter.kt`：可独立编译的真实快照加密模块，使用 Java 标准 JCA/JCE，无新增运行时依赖。
- 尚未绑定 Android Chromium 的具体消息通道；`capabilities.crypto` 默认并继续保持 `false`，适配器完成不代表 Android 浏览器完成。不得使用 WebView 替代 Chromium 或据此宣称已完成原生桥接。
- 文件能力必须使用 Storage Access Framework 返回的授权 URI；禁止把 Windows 路径模型带入 Android。
- 会话密钥使用 Android Keystore；应用生命周期恢复必须覆盖进程被系统回收、断网和权限撤销。

## 接入顺序

1. 先完成 Android Chromium 的真实消息收发和 Origin 校验。
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

`SaasBridgeContract.Crypto` 提供与网页一致的两个方法，使用 `SnapshotEncryptOptions`、`SnapshotDecryptOptions` 和 `SaasSnapshotEnvelope`。现有实现无需修改，新增的可空 `crypto` 属性默认返回 null；具备适配模块的宿主可覆盖为 `JvmSnapshotCryptoAdapter()`，但能力声明仍应保持 `crypto = false`。

后续 Chromium 宿主需在校验来源、请求类型、账号上下文及消息大小之后，将 `crypto.encryptSnapshot` 与 `crypto.decryptSnapshot` 路由到该实例，并按以上字段名传输信封和快照对象。消息层应在分配大字符串或反序列化之前限制帧大小，并拒绝非整数 `iterations`，避免将超范围数字截断为 Int。只有真实消息通道端到端验证通过后，才能将 `getCapabilities()` 的 `crypto` 改为 true。适配器的 `suspend` 方法不自行切换线程，宿主必须使用有界后台执行器，禁止在 Android 主线程执行密钥派生。

本模块没有提供 Chromium 消息通道、Origin 校验、Android 生命周期处理、原生 Tab/存储能力、SDK/NDK 构建或 APK。账号是否授权由宿主校验；加密模块只提供 AAD 绑定和明文账号一致性校验。

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
