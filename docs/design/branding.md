# jeval 标志与应用图标

更新日期：2026-10-04。状态：原创矢量标志已接入，效果待用户反馈。

<img src="../../apps/desktop/resources/jeval.svg" width="128" height="128" alt="jeval 标志">

## 设计

以字母 j 为主体：圆点表示一条可定位的记录，下方圆润的勾形呼应核验与回看执行过程。深灰底与白色符号保持高对比度，轮廓简单，适用于窗口标题栏和任务栏的小尺寸显示。

视觉参考为用户指定的 [LangSmith / LangChain 品牌页](https://www.langchain.com/brand-assets) 和 [Manus 品牌页](https://events.manus.im/brand)，取简洁、独立符号与清晰轮廓的方向。图形是为 jeval 独立绘制的 SVG，不包含这些品牌的现有图形或字标。

| 资源 | 用途 |
| --- | --- |
| [jeval.svg](../../apps/desktop/resources/jeval.svg) | 唯一矢量源；应用侧栏、页面图标和双语 README |
| [jeval.png](../../apps/desktop/resources/jeval.png) | 512px 透明背景图，供 Electron 窗口使用 |
| [jeval.ico](../../apps/desktop/resources/jeval.ico) | Windows exe 图标，含 16/20/24/32/40/48/64/128/256px 图层 |

配色为 #262725 与 #FFFFFF，保留正方形比例与外沿透明留白；不在小尺寸标志内加入完整名称。界面中符号与 jeval 文字共同组成品牌区域。

## 生成与接入

编辑 SVG 后执行 `npm run build:icons`，由 [icons.mjs](../../scripts/build/icons.mjs) 使用 resvg 生成 PNG 与多尺寸 ICO。`npm run dev` 和 `npm run build` 已自动包含此步骤，不依赖手工转换或网络字体。源图与派生资源均保存在仓库。

主进程为 BrowserWindow 指定 PNG 图标，并设置与打包配置一致的 Windows AppUserModelID。开发时读取 resources，目录包通过 extraResources 携带到 `resources/branding`。渲染层通过 Vite 导入 SVG，构建后使用本地资源。

Windows 打包配置指定 `win.icon`，并使用 `signExecutable: false`，保留图标和版本资源写入但不签名。原配置 `signAndEditExecutable: false` 会连图标资源一起跳过，不可用于当前图标构建。截图中此前的原子形图标来自 Electron 默认图标，并非项目主动设置的 React 标志。

验证结果见 [M0 进度](../development/m0-progress.md)。当前只验证 Windows 目录包；macOS ICNS、安装包和固定任务栏快捷方式的迁移不在本轮范围内。
