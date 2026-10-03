# 项目任务边界

- 只有明确属于 Android 逆向、APK 分析、抓包或动态调试的任务，才读取 `C:\Users\Administrator\.codex\android-reverse-tools.md` 并使用逆向工具链。
- Android 构建、编译、GN/Ninja、SDK/NDK 配置、平台适配和测试不属于逆向任务，不读取逆向工具说明，也不调用 JADX、APKTool、Frida 或其他逆向流程。
