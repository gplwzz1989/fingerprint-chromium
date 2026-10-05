# SaaS 浏览器壳改造计划

## 2026-10-04 源码实现与设计稿对照

`design/saas-browser-shell-desktop.svg` 是目标设计示意，不是当前客户端截图。当前状态是“结构和主要交互已有源码，设计视觉部分待落地，最新原生运行验收未完成”，不能把整张图记作已完成。

| 图中行为/区域 | 当前实现 | 仍需完成 |
| --- | --- | --- |
| 客户端启动加载 SaaS | `Browser::OnTabInsertedAt` 保证管理宿主存在；宿主按共享启动地址用全屏 iframe 加载独立 SaaS | 最新运行产物验收；已有控制台时启动一定选中它的行为需核对 |
| 同父窗口的账号原生 Tab | 已有原生账号环境和分区；Web 按账号先查已有 Tab，再激活或创建，并复用并发打开请求 | 最新运行版的幂等、正常导航和关闭回归 |
| 固定且不可关闭的管理 Tab | 创建后 `SetTabPinned(..., true)`，原生关闭策略拒绝普通关闭，整窗退出正常 | 验证按钮/快捷键/菜单及取消固定后的视觉和保护 |
| 顶部完整标题的宽管理 Tab | 当前使用上游 pinned 标签，通常为紧凑图标，不等同图中宽标签 | 桌面原生专用视觉与固定策略，不能只改 SaaS CSS 达到图中效果 |
| 账号 Tab 压缩 | 原生已有上游宽度分配；SaaS 页面内另有真实 Tab 映射条与 CSS 省略 | 原生多 Tab 的宽度、完整标题提示及无横向滚动实际验收 |
| SaaS 左侧导航和筛选区域 | 当前为 252px 单侧栏、账号搜索/权限筛选；已有账号目录、同步和真实运行状态 | 图中的窄图标栏 + 账号筛选侧栏尚未按此布局实现；平台/分组筛选不能用设计示意填充假数据 |

核对入口：`chrome/browser/ui/browser.cc`、`ui/tabs/tab_strip_model.cc`、`ui/views/tabs/tab.cc`、`resources/fingerprint_manager/fingerprint_manager_ui.*` 和 `saas-web/app.js/index.html/styles.css`。当前 Release/publish 程序仍为 2026-09-30，Development 为 2026-10-02；本次未启动浏览器或编译，不能据源码宣称运行版与图一致。

图中首个 SaaS Tab 还画有“×”，与不可关闭目标冲突；后续视觉落地须遵守不可关闭约束，不按该示意添加关闭入口。本次保留原设计文件，仅澄清状态。剩余视觉工作和运行验证纳入 [开发计划 T05](development-plan.md)，页面内 Tab 条与浏览器最上方原生 TabStrip 分别处理。

## 一、产品结构

目标结构是“单父窗口 + 固定 SaaS 首页 Tab + 账号环境 Tab”。SaaS 首页负责账号目录、权限、同步、审计和设备管理；账号环境 Tab 只承载对应账号的网站页面和原生隔离能力。

第一 Tab 固定为 SaaS 首页：启动时创建或激活唯一的管理 Tab，不允许关闭，不参与账号环境同步，也不进入账号环境列表。所有账号环境使用稳定 `account_id`，同一账号最多存在一个运行中的 Tab。

## 二、核心交互

| 场景 | 行为 |
| --- | --- |
| 启动 | 检查父窗口 Tab 列表，确保唯一 SaaS 管理 Tab 存在并激活；已有则直接复用 |
| 打开账号 | 先按 `account_id` 查找已有 Tab；存在则激活，不存在才创建 |
| 点击运行中账号 | 直接激活对应 Tab，SaaS 页面不重复创建环境 |
| 关闭账号 Tab | 只关闭当前环境 Tab，不删除账号目录、云端快照或权限 |
| 关闭 SaaS 首页 | UI 不提供关闭入口；原生层拒绝关闭或导航管理 Tab |
| Tab 数量增加 | Tab 栏不出现横向滚动条；按可用宽度动态压缩 Tab 标题，最小宽度后使用省略号和悬浮完整标题 |
| 切换工作区 | 先清理不可见账号 Tab 的业务操作状态，再刷新 Tab 映射；不关闭不属于当前工作区的真实 Tab，避免误操作 |

## 三、实现阶段

1. 浏览器壳与 Tab 状态模型：增加固定 `manager` Tab 标识、`account_id → tab_id` 映射、启动恢复和幂等创建。
2. Tab 栏视觉与行为：固定管理 Tab、关闭按钮策略、动态宽度、标题省略、激活态和键盘导航。
3. SaaS 页面接入：账号卡片和“运行中 Tab”统一调用 `activateOrCreateAccountTab(account)`，不再由不同入口各自创建。
4. 原生边界保护：管理 Tab 禁止关闭/导航；账号 Tab 关闭只影响本地运行态，不删除 SaaS 数据。
5. 状态与回归：覆盖启动、重复点击、多个 Tab 压缩、关闭后重开、会话切换和原生桥不可用状态。

## 四、关键文件

- `saas-web/app.js`：SaaS 页面启动、账号打开和已有 Tab 激活。
- `saas-web/operations.js`：运行 Tab 列表、切换、关闭和状态刷新。
- `saas-web/styles.css`：Tab 栏压缩、标题省略、焦点和移动端布局。
- `saas-web/bridge-contract.js/.d.ts`：桥接接口保持业务无关，只提供 Tab 能力。
- `patches/series` 及 `patches/upstream-fixes/`：Chromium 管理 Tab 的不可关闭、单账号 Tab 和父窗口约束。

## 五、验收标准

- 启动后始终只有一个可见 SaaS 首页 Tab，且没有关闭按钮。
- 连续点击同一账号 10 次，仍只有一个对应账号 Tab。
- 账号卡片、运行 Tab 列表和恢复流程对同一账号使用同一 Tab。
- 8 个以上 Tab 时无横向滚动条，Tab 标题按宽度压缩并显示省略号。
- 点击已有账号时可观察到激活切换，不触发新的 Tab 创建请求。
- 关闭账号 Tab 后，账号目录、快照、授权和审计数据仍保留。

设计稿：

- [默认 SaaS 首页与浏览器壳](design/saas-browser-shell-desktop.svg)
- [多环境 Tab 压缩状态](design/saas-browser-shell-compressed.svg)
