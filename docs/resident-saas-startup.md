# 常驻 SaaS 控制台与默认地址

PC 和 Android 客户端使用同一个编译时默认地址。地址集中在 Chromium 源码 `chrome/common/chrome_switches.cc`：

```cpp
const char kFingerprintSaasStartupUrl[] = "http://127.0.0.1:8787/";
```

当前地址沿用本机开发服务。移动设备中的 `127.0.0.1` 指向移动设备自身，部署版应将这一常量改为实际 SaaS 服务地址。地址不保存在用户设置、命令行或前端资源中。

## 修改与编译范围

首次接入启动流程和关闭保护涉及若干局部源文件。接入完成后，仅更换默认地址时：

1. 只修改上述 `.cc` 中的常量，不修改声明头文件、GN 参数或 Java/TypeScript。
2. 每个平台仅重新编译 `chrome_switches.cc` 对应对象文件。
3. 更新包含该对象的静态库，并重新链接 PC 二进制或 Android 原生库；Android 发布产物还需要重新打包和签名。

单个对象文件并不能直接改变已生成的 `chrome.exe` 或 APK。重新链接是让地址进入可执行产物的必要步骤，但不意味着其余源文件全量重编译。

Windows 上复用现有 Development 参数，验证和编译常量文件：

```powershell
python -X utf8 utils/check_cpp_syntax.py chrome/common/chrome_switches.cc
python -X utf8 utils/check_cpp_syntax.py chrome/common/chrome_switches.cc --compile
```

上述工具读取已有 Ninja 编译参数，直接调用本地编译器，不执行 GN、不自动生成依赖，也不自动链接。首次接入之后，应在已完成构建的同平台输出目录中做局部更新。

`patches/upstream-fixes/managed-tab-resident-saas-startup.patch` 是桌面与地址常量的持久化补丁；修改默认地址时也需同步其常量行，保证清洁源码能重放。`android-resident-saas-startup.patch` 只引用共享常量，不需要随地址调整。

## 常驻规则

- 桌面端自动创建一个固定的控制台宿主页，并加载独立 SaaS Web；已有宿主页时不重复创建。
- 控制台 Tab 固定且禁止普通关闭操作；整窗退出和进程退出仍正常释放。
- 地址栏导航从控制台进入新的隔离 Tab，不替换控制台宿主页。
- Android 等待 Tab 模型和会话恢复完成后打开或选中控制台；关闭过滤基于 Tab 对象身份，站内跳转不会解除保护。
- 默认地址对应的 Origin 自动加入桌面能力授权列表，附加站点仍采用现有编译时白名单。

## 验证状态

桌面 C++ 局部语法编译、默认地址常量文件的单文件对象编译、TypeScript 静态检查、Android JNI 接口生成，以及两个启动补丁的反向应用检查均已通过。最新桌面二进制尚未重新链接，Android 完整 Java/C++ 编译、APK 打包和运行验收尚未完成；这些结果不能替代运行版关闭保护测试。
