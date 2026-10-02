# Android 原生桥接适配层

这里保存与 `saas-web/bridge-contract.d.ts` 对齐的 Android 契约和适配边界。当前阶段不复制 Chromium 桌面输出，也不伪造 Android 编译结果。

## 当前状态

- `SaasBridgeContract.kt`：平台无关的 Kotlin 接口和数据结构，文件与 HTTP 字段和桌面 Web 契约保持对应。
- 尚未绑定 Android WebView/Chromium 的具体消息通道；需要 Android SDK、NDK 和 Chromium Android 目标工具链后再接入。
- 文件能力必须使用 Storage Access Framework 返回的授权 URI；禁止把 Windows 路径模型带入 Android。
- 会话密钥使用 Android Keystore；应用生命周期恢复必须覆盖进程被系统回收、断网和权限撤销。

## 接入顺序

1. 先完成 Android WebView/Chromium 的 `postMessage` 收发和 Origin 校验。
2. 接入 `tabs`、`storage`、`fingerprint` 三项已稳定桌面契约。
3. 增加 Keystore 会话、应用恢复和账号目录隔离测试。
4. 最后实现 SAF 文件能力和可取消的原生 HTTP，并执行权限撤销与审计测试。
