# 指纹多 Tab 开发与隔离测试报告

## 1. 文档范围

本文记录当前开发阶段的构建策略、单父窗口原生 Tab 实现状态，以及使用 BrowserLeaks JavaScript 测试页对两个账号 Tab 进行的指纹和存储隔离验证。

测试日期：2026-10-02
测试构建：`out/Development` 开发版
测试页面：[BrowserLeaks JavaScript](https://browserleaks.com/javascript)

本文只记录已实际验证的能力。未验证或存在问题的功能不会标记为完成。

## 2. 构建策略

### 2.1 开发版

- 输出目录：`build/src/out/Development`
- 使用组件化编译：`is_component_build=true`
- 关闭 Widevine：`enable_widevine=false`
- 目标是快速局部编译和增量验证。
- 已成功生成开发版 `chrome.exe` 和 `chrome.dll`。
- 后续小功能开发优先只编译受影响的目标，不主动进行全量编译。

### 2.2 Release 版

- 输出目录：`build/src/out/Release`
- 正式构建通过 `is_official_build=true` 关闭组件化编译，即 `is_component_build=false`。
- 用于发版前的整体编译、安装包制作和最终回归测试。
- 在功能开发全部完成前，不以 Release 全量编译作为日常验证手段。

### 2.3 GN 变更规则

- 小功能修改优先局部编译，不重新生成 GN。
- 如果配置修改必然导致大范围重新编译，必须先说明原因、影响范围和预计成本，并取得明确许可。
- 当前测试没有修改 GN，也没有重新生成 GN。

## 3. 当前功能结构

- 整个浏览器使用一个父窗口。
- 账号环境使用同一窗口内的原生 Tab 展示。
- 每个受管理 Tab 使用独立、持久化的 Storage Partition。
- 每个 Tab 拥有独立的账号标识、指纹种子、Cookie、LocalStorage、SessionStorage 和页面状态。
- 管理页面通过现有 WebUI 接口创建、激活、导航和关闭受管理 Tab。
- 当前管理页面已经有指纹配置接口，但页面上还没有完整的指纹编辑表单。

## 4. 双 Tab 指纹测试

测试中创建了两个访问同一测试页面的受管理 Tab，并设置了不同的指纹配置：

| 项目 | Tab 1 | Tab 2 |
|---|---|---|
| 指纹种子 | `59608448` | `1419342226` |
| 配置的 User-Agent | Chrome 120 | Chrome 131 |
| 配置的硬件并发数 | `8` | `16` |
| 实际 `navigator.hardwareConcurrency` | `8` | `16` |
| Canvas 测试哈希 | `432d3ed7` | `1a27446f` |

测试结果表明：

1. 两个 Tab 的硬件并发数配置可以独立生效。
2. 两个不同指纹种子生成了不同的 Canvas 结果。
3. 两个 Tab 可以在同一个父窗口中同时运行，互不要求独立浏览器进程。

## 5. 存储隔离与快照测试

### 5.1 读取隔离

在 Tab 1 写入：

- Cookie：`fp_test_cookie=tab1`
- LocalStorage：`cross_tab_probe=only-tab-1`

切换到 Tab 2 后读取结果：

- Cookie：不存在
- `cross_tab_probe`：`null`

这证明两个 Tab 的 Cookie 和 LocalStorage 没有共享。

### 5.2 管理器读取

通过管理页面的存储快照接口读取两个 Tab：

- Tab 1 能读到自己的 Cookie、LocalStorage 和指纹配置。
- Tab 2 只能读到自己的 LocalStorage 和指纹配置，读不到 Tab 1 的 Cookie。

### 5.3 管理器写入

向 Tab 2 写入：

```text
server_sync_probe=written-to-tab-2
```

随后重新读取 Tab 2 快照，能够读回该值，说明当前快照写入链路可用，具备后续接入 SaaS 同步的基础。

## 6. 已发现的问题

### 6.1 User-Agent 尚未反映到页面 JavaScript

虽然管理器状态和存储快照中已经保存了 Tab 1、Tab 2 不同的 User-Agent 配置，但重新加载和重新导航后，页面中的：

```js
navigator.userAgent
```

仍显示当前 Chromium 的 Chrome 144 字符串，两个 Tab 没有显示配置的 Chrome 120 和 Chrome 131。

当前结论：

- User-Agent 配置的保存链路已工作。
- User-Agent 配置对页面 JavaScript 的实际应用尚未完成。
- 在修复前，不能宣称“每个 Tab 的完整 User-Agent 指纹已经隔离”。

初步定位为现有 `SetUserAgentOverride` 应用路径没有正确标记当前导航条目，导致渲染进程没有在页面环境中使用该覆盖值。该问题需要单独修复并重新进行局部编译验证。

### 6.2 指纹配置界面不完整

当前管理页面可以创建和管理 Tab，但还没有面向用户的 User-Agent、硬件并发数、平台、时区和指纹种子编辑界面。现阶段只能通过已有 WebUI 接口进行测试配置。

## 7. 当前结论

已验证完成：

- 单父窗口中的多个原生 Tab。
- 每个 Tab 独立的持久化存储分区。
- Cookie 和 LocalStorage 的跨 Tab 隔离。
- 存储快照读取。
- 存储快照写入。
- 硬件并发数的 Tab 级指纹配置。
- Canvas 指纹种子差异。
- 指纹配置随账号快照保存。

尚未完成：

- User-Agent 对页面 JavaScript 的实际覆盖。
- 面向用户的指纹配置编辑界面。
- IndexedDB、Cache Storage、Service Worker 等更复杂存储的跨设备同步。
- Release 版本的完整编译、安装包制作和整体回归测试。

## 8. 后续开发顺序

1. 修复 User-Agent 覆盖链路，优先进行局部编译和 BrowserLeaks 回归验证。
2. 在管理页面增加指纹配置表单，并复用现有 WebUI 接口。
3. 验证 `window.open`、`target=_blank` 和会话恢复是否始终保持在同一父窗口及正确账号 Tab 中。
4. 完成 SaaS 快照加密、上传、恢复和冲突处理测试。
5. 所有功能完成后，再申请 Release 全量编译和发版回归测试。
