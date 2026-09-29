# 项目指纹修改点总结

## 1. 结论与版本范围

本项目基于 Ungoogled Chromium，通过补丁修改 Chromium、Blink、V8 和 WebGL 的运行逻辑，实现浏览器指纹伪装、指纹噪声和部分自动化隐藏能力。

需要注意当前仓库的版本结构：

- 当前检出版本为 `3f61b0df`（Chrome 148）。该提交只保留了发布说明、许可证和图片，源码补丁已按项目发布流程从当前检出中移除。
- 完整的指纹补丁可以在历史提交 `831623f2`（Chrome 144.0.7559.132）中看到。本文的“修改文件”和“修改方式”以该完整补丁集为主要依据，并结合当前 `README-ZH.md` 的功能说明整理。
- 因此，下表中的 Chromium 源文件路径是“补丁应用时的目标文件”，不是当前工作区中实际存在的文件。当前工作区新增的只有本文档。

补丁通过 `patches/series` 按顺序应用。总体链路如下：

```text
启动参数
  └─ 浏览器进程注册并向渲染进程转发
       └─ Blink / V8 / WebGL 在具体 API 返回值处读取参数
            └─ 返回伪装后的平台、硬件、图像、尺寸或时间信息
```

## 2. 指纹修改总表

| 指纹或隐私维度 | 补丁文件 | 主要修改文件 | 如何修改 | 可以达到的效果 |
|---|---|---|---|---|
| 指纹总开关、种子和参数转发 | `patches/extra/fingerprint/000-add-fingerprint-switches.patch` | `components/ungoogled/ungoogled_switches.cc`、`components/ungoogled/ungoogled_switches.h`、`content/browser/renderer_host/render_process_host_impl.cc` | 注册 `--fingerprint`、品牌、平台、CPU 核心数、时区、禁用伪装等参数，并将参数从浏览器进程转发到渲染进程。 | 让各个渲染层能够使用同一个指纹种子；未启用 `--fingerprint` 时，大多数自定义伪装逻辑不生效。 |
| User-Agent、平台和 Client Hints | `patches/extra/fingerprint/002-user-agent-fingerprint.patch` | `components/embedder_support/user_agent_utils.cc`、`components/ungoogled/fingerprint_data.h`（新增）、`content/browser/client_hints/client_hints.cc`、`third_party/blink/common/user_agent/user_agent_metadata.cc`、`third_party/blink/public/common/user_agent/user_agent_metadata.h`、`third_party/blink/renderer/core/execution_context/navigator_base.cc`、`navigator.cc`、`navigator_ua.cc`、`frame_fetch_context.cc` | 根据品牌和版本生成 User-Agent、UA Metadata、品牌列表和 GREASE 品牌顺序；用种子选择 Chromium 版本、平台版本和品牌顺序；同时覆盖 `navigator.userAgent`、`navigator.platform`、`navigator.userAgentData` 以及请求中的 Client Hints。 | 可以把浏览器伪装成 Windows、Linux 或 macOS，并保持 `User-Agent`、`navigator`、UA-CH 之间的主要信息一致，降低直接读取这些字段得到真实环境的概率。 |
| 操作系统版本与浏览器品牌 | 同上 | `components/ungoogled/fingerprint_data.h`、`components/embedder_support/user_agent_utils.cc`、Blink User-Agent Metadata 相关文件 | 内置 Windows、Linux、macOS 的平台版本列表，以及 Chrome、Edge、Opera、Vivaldi 的品牌名称、版本和 UA 后缀；自定义版本优先于种子选择。 | 支持通过 `--fingerprint-platform`、`--fingerprint-platform-version`、`--fingerprint-brand` 和 `--fingerprint-brand-version` 模拟不同浏览器环境。 |
| 音频指纹 | `patches/extra/fingerprint/003-audio-fingerprint.patch` | `third_party/blink/renderer/modules/webaudio/offline_audio_context.cc` | 将指纹种子与采样率组合后哈希，生成稳定的微小采样率扰动，范围约为 `±0.01`；使用 `--disable-spoofing=audio` 时跳过扰动。补丁中还定义了基于帧数的噪声计算函数。 | 同一指纹种子在离线音频渲染中产生稳定但不同于真实机器的结果，使音频哈希不再直接暴露本机音频栈特征。 |
| CPU 核心数和设备内存 | `patches/extra/fingerprint/005-hardware-concurrency-fingerprint.patch` | `third_party/blink/renderer/core/frame/navigator_concurrent_hardware.cc`、`navigator_device_memory.cc` | `--fingerprint-hardware-concurrency` 优先使用指定核心数；否则根据种子计算偶数核心数；启用种子时绕过真实处理器数量。设备内存直接返回固定值 `8`。 | 修改 `navigator.hardwareConcurrency` 和 `navigator.deviceMemory`，避免网站直接通过 CPU、内存规模建立本机画像。 |
| 字体指纹 | `patches/extra/fingerprint/006-font-fingerprint.patch` | `third_party/blink/renderer/platform/fonts/font_cache.cc` | 内置 Windows、macOS、Linux 字体集合；根据目标伪装平台和种子决定字体替换或隐藏。基础字体和最后手段字体保留；跨平台字体默认隐藏，平台字体用当前系统字体替代；`--disable-spoofing=font` 可关闭。 | 减少通过字体枚举识别真实操作系统和安装软件的能力，使字体集合更接近目标平台。字体渲染和字体可用性可能因此发生变化。 |
| Canvas `getImageData` 与图像像素 | `patches/extra/fingerprint/012-canvas-get-image-data.patch`、`patches/extra/bromite/flag-fingerprinting-canvas-image-data-noise.patch` | `third_party/blink/renderer/modules/canvas/canvas2d/base_rendering_context_2d.cc`、`third_party/blink/renderer/platform/graphics/image_data_buffer.cc`、`static_bitmap_image.cc`、`static_bitmap_image.h`、`third_party/blink/renderer/platform/image-encoders/image_encoder.cc` | 在 Canvas 像素读回和图像处理路径中调用统一的像素扰动函数；使用种子、坐标和颜色通道生成稳定哈希，只修改少量边缘像素的最低有效位，跳过纯黑和纯白像素，并将修改数量限制在小范围内。 | 使 Canvas 像素哈希随指纹种子变化，同时尽量保持图像肉眼不可见的差异；`--disable-spoofing=canvas` 可关闭。 |
| Canvas `toDataURL` | `patches/extra/fingerprint/013-canvas-toDataURL.patch` | `third_party/blink/renderer/core/html/canvas/html_canvas_element.cc` | 读取原图像到独立的 RGBA 缓冲区，只对副本施加 Canvas 噪声，再用修改后的副本生成新的图片对象和 Data URL，不直接改写原始图像。 | 让通过 `canvas.toDataURL()` 获取的结果也带有稳定噪声，同时降低对页面原始图像数据的副作用。 |
| Canvas 文本测量 | `patches/extra/fingerprint/015-canvas-measure-text.patch`、`patches/extra/bromite/fingerprinting-flags-client-rects-and-measuretext.patch` | `third_party/blink/renderer/modules/canvas/canvas2d/base_rendering_context_2d.cc`、`third_party/blink/renderer/core/html/canvas/text_metrics.cc`、`text_metrics.h` | 在 `CanvasRenderingContext2D.measureText()` 返回的文本指标上增加基于种子的极小宽度扰动，约为 `0.00001` 量级。 | 降低通过字体、渲染器和文本排版测量结果反推出真实环境的准确度；过小的扰动主要用于改变哈希，不应明显影响页面布局。 |
| ClientRects 和几何尺寸 | `patches/extra/fingerprint/014-client-rects.patch`、`patches/extra/bromite/fingerprinting-flags-client-rects-and-measuretext.patch` | `third_party/blink/renderer/core/dom/document.cc`、`document.h`、`element.cc`、`element.h`、`range.cc`、`ui/gfx/geometry/quad_f.cc`、`quad_f.h` | 根据种子分别生成 X、Y 方向的微小偏移，范围约为 `±0.001` 像素；应用到元素、Range 和 Quad 的几何返回值。对部分绝对定位且 `top`、`left` 可确定的元素跳过偏移；`--disable-spoofing=clientrects` 可关闭。 | 让 `getClientRects()`、`getBoundingClientRect()` 等结果不再稳定暴露真实布局计算细节，降低几何指纹的可重复性。 |
| WebGL 图像指纹 | `patches/extra/fingerprint/016-webgl-readPixels.patch` | `third_party/blink/renderer/modules/webgl/webgl_rendering_context_base.cc`，复用 `third_party/blink/renderer/platform/graphics/static_bitmap_image.cc` | 在 `WebGLRenderingContext.readPixels()` 返回数据后，根据 WebGL 像素格式调用与 Canvas 相同的颜色通道扰动逻辑；该路径检查的是 `--disable-spoofing=canvas`。 | 改变 WebGL 像素读回结果，降低通过 WebGL 渲染图像生成固定哈希的能力。 |
| WebGL 供应商和渲染器 | `patches/extra/fingerprint/011-gpu-info.patch` | `components/ungoogled/fingerprint_data.h`、`third_party/blink/renderer/modules/webgl/gpu_fingerprint.cc`、`gpu_fingerprint.h`、`gpu_info.cc`、`gpu_info.h`、`webgl_rendering_context_base.cc`、`webgl/BUILD.gn` | 新增 GPU 型号和设备 ID 数据表，根据种子选择 GPU；按 Windows、Linux、macOS 生成不同格式的 `GL_VENDOR` 和 `GL_RENDERER` 字符串，并在 WebGL `getParameter()` 查询时返回伪装值。`--disable-spoofing=gpu` 可关闭。 | 修改 WebGL Debug Renderer Info 暴露的供应商、显卡型号和渲染器字符串，减少真实 GPU、驱动和操作系统组合被直接识别的可能。 |
| 时区 | `patches/extra/fingerprint/018-timezone.patch` | `third_party/blink/renderer/core/timezone/timezone_controller.cc` | 启动时读取 `--timezone`，覆盖当前时区 ID，并同步设置 ICU 和 V8 的时区；后续获取当前时区时优先返回命令行指定值。 | 使 JavaScript 时区、日期格式化和浏览器内部时区表现接近指定地区，避免暴露系统真实时区。 |
| WebRTC 本地地址暴露 | `patches/extra/ungoogled-chromium/default-webrtc-ip-handling-policy.patch` | `chrome/browser/ui/browser_ui_prefs.cc` | 将 WebRTC 默认 IP 处理策略改为禁止非代理 UDP 连接，对应 README 中建议保留的 `--disable-non-proxied-udp` 策略。 | 减少 WebRTC 绕过代理暴露本地或真实网络地址的概率；它主要保护网络地址，不会改变浏览器指纹字段。 |
| Client Hints 主动关闭 | `patches/extra/ungoogled-chromium/add-flag-to-remove-client-hints.patch` | `chrome/browser/ungoogled_flag_entries.h`、`content/browser/client_hints/client_hints.cc`、`third_party/blink/common/features.cc`、`features.h`、`navigator_ua.cc`、`frame_fetch_context.cc` | 新增 `RemoveClientHints` 特性；启用后停止请求头、资源请求和 `navigator.userAgentData` 的 Client Hints 输出。该功能是独立开关，不等同于 `--fingerprint` 自动开启。 | 在不需要 Client Hints 的场景下减少向网站发送的平台、设备和浏览器信息。 |
| 语言信息 | 无独立指纹补丁，使用 Chromium 原生参数 | 主要由 Chromium 的语言配置和网络请求逻辑处理 | 通过 `--lang` 设置浏览器语言，通过 `--accept-lang` 设置请求接受语言；项目未在指纹补丁中对语言值做种子随机化。 | 可以让界面语言和 `Accept-Language` 接近目标环境，但需要同时配置两者，不能仅依赖 `--fingerprint`。 |
| 插件信息 | `patches/extra/fingerprint/004-plugin-fingerprint.patch.deprecated` 未加入 Chrome 144 的 `patches/series` | 当前没有活动的插件指纹修改文件 | Chrome 133 及以上版本由浏览器返回固定插件列表；旧插件补丁已标记为废弃。 | 插件列表不再直接反映本机插件差异，但这不是当前指纹补丁新增的运行时逻辑。 |
| 关闭 `navigator.webdriver` 的自动化标记 | `patches/extra/fingerprint/009-webdriver.patch` | `third_party/blink/renderer/core/frame/navigator.cc` | 删除 `AutomationControlledEnabled()` 为真时直接返回 `true` 的分支，改为只返回自动化覆盖探针的结果。 | 常规自动化控制下 `navigator.webdriver` 不再必然为 `true`；其他自动化痕迹仍可能存在。 |
| Headless User-Agent | `patches/extra/fingerprint/010-headless.patch` | `headless/lib/browser/headless_browser_impl.cc` | 将默认 UA 产品名从 `HeadlessChrome` 改为 `Chrome`。 | 隐藏 UA 中最明显的 `HeadlessChrome` 字样；README 已明确说明其他 Headless 特征没有修改。 |
| CDP Runtime 检测 | `patches/extra/fingerprint/001-disable-runtime.enable.patch` | `v8/src/inspector/v8-runtime-agent-impl.cc`、`v8-runtime-agent-impl.h` | 禁止 Runtime Agent 添加绑定、关闭消息报告，并让 `enabled()` 固定返回 `false`。 | 减少调用 CDP `Runtime.enable` 后出现的典型检测信号，但会改变部分 DevTools/调试 Runtime 行为。 |
| Closed Shadow DOM 自动化访问 | `patches/extra/fingerprint/007-shadow-root.patch` | `third_party/blink/renderer/core/dom/element.cc`、`element.h`、`element.idl` | 新增 `fakeShadowRoot` Web IDL 属性，以 `PerWorldBindings` 方式映射到获取 Shadow Root 的方法。 | 自动化脚本可以通过 `fakeShadowRoot` 访问 Closed Shadow Root，便于测试和操作；这属于自动化辅助能力，不是随机化指纹值。 |

## 3. 参数与生效范围

| 参数 | 作用 | 备注 |
|---|---|---|
| `--fingerprint=<seed>` | 指定指纹种子，开启大部分指纹伪装 | 同一个种子会产生稳定结果；不同种子通常产生不同结果。 |
| `--fingerprint-platform=windows\|linux\|macos` | 指定目标操作系统 | 影响 User-Agent、平台字符串、字体和 GPU 字符串。 |
| `--fingerprint-platform-version=<版本>` | 指定目标平台版本 | 未指定时由内置列表和种子选择。 |
| `--fingerprint-brand=Chrome\|Edge\|Opera\|Vivaldi` | 指定浏览器品牌 | 会影响 User-Agent、品牌列表和 UA Metadata。 |
| `--fingerprint-brand-version=<版本>` | 指定品牌版本 | 未指定时使用补丁中的默认版本。 |
| `--fingerprint-hardware-concurrency=<整数>` | 指定 `navigator.hardwareConcurrency` | 优先级高于种子自动计算值。 |
| `--disable-spoofing=font,audio,canvas,clientrects,gpu` | 选择性关闭对应指纹伪装 | 多个值用逗号分隔；Canvas 选项也控制 WebGL `readPixels` 的像素扰动。 |
| `--timezone=<IANA 时区>` | 覆盖浏览器时区 | 例如 `Asia/Shanghai`、`UTC`。 |
| `--lang=<语言代码>` | 设置浏览器语言 | 例如 `zh-CN`；属于 Chromium 原生参数。 |
| `--accept-lang=<语言列表>` | 设置接受语言 | 例如 `zh-CN,en-US`；应与目标环境保持一致。 |
| `--disable-non-proxied-udp` | 限制 WebRTC 非代理 UDP | 用于减少真实网络地址泄漏，建议与代理配置一起使用。 |
| `--fingerprint-screen-width`、`--fingerprint-screen-height`、`--fingerprint-device-scale-factor`、`--fingerprint-location` | 在总开关补丁中声明并转发 | 在 Chrome 144 完整指纹补丁中只能找到参数声明和渲染进程转发，未找到实际读取和应用逻辑，因此不能据此认定屏幕或地理位置已被修改。 |

## 4. 预期效果和限制

### 能达到的效果

- 将同一个指纹种子贯穿 User-Agent、平台、硬件、字体、Canvas、WebGL、音频、ClientRects 和时区等多个 API，减少不同 API 之间的明显矛盾。
- 让 Canvas、WebGL 和音频结果在同一配置下保持稳定，在不同种子之间产生可区分的结果，适合指纹隔离、自动化测试和环境模拟。
- 支持以命令行方式切换平台、品牌、版本、CPU 核心数和时区，便于批量创建不同浏览器配置。
- 通过限制 WebRTC 非代理 UDP、关闭 Client Hints 和隐藏部分自动化标记，减少网络地址和自动化特征暴露。

### 不能保证的效果

- 这不是完整匿名化方案，IP 地址、代理质量、TLS/HTTP2 特征、屏幕实际尺寸、字体渲染差异、扩展列表、WebGPU、媒体设备和系统级特征仍可能泄漏。
- Headless 补丁只移除 `HeadlessChrome` 字样，README 已明确提示其他 Headless 特征仍然存在。
- 指纹噪声会改变 Canvas、WebGL、音频和几何测量结果，可能影响依赖像素精确比较、图形测试或排版测量的页面。
- 字体替换和 GPU 字符串是基于内置数据表生成，并不代表真实目标设备的全部驱动、字体和渲染特征。
- `--fingerprint-screen-width`、`--fingerprint-screen-height`、`--fingerprint-device-scale-factor` 和 `--fingerprint-location` 在可见补丁中没有实际生效代码，不能作为已完成的屏幕或位置伪装功能使用。
- 当前工作区没有编译产物或源码补丁，本文没有进行运行时验证；实际效果仍应使用 creepjs、BrowserLeaks、PixelScan 等工具在对应构建版本上复测。

## 5. 推荐验证项目

| 验证类别 | 建议检查内容 |
|---|---|
| 基础身份 | `navigator.userAgent`、`navigator.platform`、`navigator.userAgentData`、请求中的 `Sec-CH-UA`。 |
| 硬件和平台 | `hardwareConcurrency`、`deviceMemory`、字体枚举、WebGL vendor/renderer。 |
| 图像和渲染 | Canvas `getImageData`、`toDataURL`、文本测量、ClientRects、WebGL `readPixels`。 |
| 音频和时区 | OfflineAudioContext 渲染结果、`Intl.DateTimeFormat().resolvedOptions().timeZone`。 |
| 网络隐私 | WebRTC ICE 候选地址、代理是否生效、非代理 UDP 是否被限制。 |
| 自动化特征 | `navigator.webdriver`、Headless UA、CDP `Runtime.enable` 行为以及 Closed Shadow DOM 访问。 |

