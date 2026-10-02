# TOO-519–522 已批准设计的正式 Web 接入 QA（2026-10-03）

用户已批准全站Storybook并授权接入正式前端。本轮实现使用真实中心HTTP查询、原合成SQLite及既有浏览器授权，八个工作台页面与独立配对表单全部适配。6007仍为独立合成设计；18085已托管本轮业务构建，业务入口不导入`src/design`或样本。以下结论为开发验证，不代替Master TOO-477的MySQL、三机和生产验收。

## 对照证据与状态

- source visual truth path：`.artifacts/multi-machine/storybook-20261003/overview-final.png`及`curve-design.png`。后者是用户最终认可的Mac四指标/四类曲线/推算与同进度对比布局，覆盖设计阶段较早的账号截图。其余页面参考同目录各页面`*-final.png`。
- implementation screenshot path：`.artifacts/multi-machine/web-approved-20261003/overview-final.png`、`overview-viewport-final.png`、`accounts-final.png`、`accounts-mobile-final.png`及`pace-focus.png`；同目录其余页面截图与交互证据按各次修改时间保留，不把较早文件宣称为最后修订的截图。
- full-view comparison evidence：`comparison-overview.png`，左已批准概览、右实际业务；focused comparison：`comparison-header.png`与`comparison-pace.png`。均已实际打开检查。
- viewport与归一：Chrome桌面1487×1058 CSS px、deviceScaleFactor 1，内容截图宽1472px；批准概览高2175px、实际概览高2118px，比较保留自然像素、补齐底部并分别标记。批准节奏组件1200×668与实际约896px宽的账号内节奏区域分别保留自然像素，不能将容器宽度差异当作字重误差。窄屏390×844 CSS px，document宽375px（15px滚动条）；临时视口均已清除。
- state：浅色、近30日、Asia/Shanghai；设计使用固定样本，业务读取原合成中心的18会话、三平台及三台合成设备。实际账号过期/陈旧，所以主额度未知、预测暂停，当前曲线不绘制；批准样本的可信当前曲线不能复制到此状态。真实API成本、来源费用、模型未知桶及原观测时间均保留。

## Findings与修正

1. [P2] 390px自定义日期浮层展开双月横排，将document撑到717px。修正为仅作用于统计日期popup的上下月/横向预设布局和有限高度内部滚动；实机读回浮层宽320px、document375px，见`custom-range-mobile-final.png`。
2. [P2] 实际模型只有一个已知日桶时，隐藏全部marker导致折线区域看起来为空。修正为单桶显示小标记；密集曲线仍隐藏圆点，未知日保持断点，不追加事实。
3. 对照发现首轮曲线颜色接近、来源表格过重、平台明细遗漏会话列、工具技能缺切换。已用可区分配色和稳定模型颜色、简洁来源信息行、真实会话数与工具/技能切换对齐；保留完整独立分布和原始来源证据。
4. 记录搜索移入左列表后，清除全部须同时清除实际筛选与输入内容。已同步受控搜索/模型字段，恢复初始查询范围；命中缓存时可复用已有结果，不为测试人为增加重复HTTP请求。
5. 左列表隐藏表头的CSS作用域已限制为左侧顶层列表，详情中的模型/TPS表头继续保留。配对和订阅使用Modal；记录详情继续左右分屏，未使用侧边详情弹窗。
6. 范围/模型缓存指标在既有查询中缺失，不能用设计样本代替。补充可选Server字段和共享Go大整数计算：Codex全部计数完整才求比值；混合平台、其他平台、部分未知和零输入保留原因。会话生命周期指标不随范围重算。

## 必查视觉表面

- 字体与间距：系统中文字体、208px侧栏、28px桌面内容边距、单一页标题；紧凑指标/表格和次级证据可读。概览热力图、摘要、模型折线、明细/来源、两个独立分布和工具完整保留，页面副标题已删除。
- 颜色与图表：模型蓝/绿/紫/金等区分，未归因模型灰；节奏本周期蓝实线、上一周期灰蓝实线、历史中位紫虚线、理想节奏灰点线。当前未知不呈现假正常；模型未知日断点与节奏视觉连线各自遵循已确认语义。
- 图标与资产：沿用官方AntD图标，无装饰头像/插画。节奏说明图标支持hover/click/focus；Credits详情状态是小标签，主要可用库存与原观测分层，到期表没有单页分页。
- 文案与单位：Token与Mac一致使用万/亿，API金额两位小数；精确原值保留于服务端和可查证据。reset、Credits到期及自定义订阅日期分开；来源只说明采集证据，不宣称设备在线或执行归属。
- 响应式与交互：八个工作台页面桌面与390px检查，未见整页横向溢出；全年日历、宽表及账号选择仅自身滚动。项目/会话窄屏进入/返回，订阅保存/刷新读回和原备注恢复、价格目录切换、真实搜索清除、来源与历史展开均有实际操作证据。

## 开发验证

Pass：44个不同Web行为场景在受影响文件分批通过，不宣称一次全套运行；后续修改仅复跑受影响场景。最终视觉细节为Overview/Usage/SourceTable/Quota四文件15场景，记录筛选为Records五场景，日志分别为`visual-refinements.log`和`records-final.log`。TypeScript、全src AntD lint（0问题）与最终业务Vite构建通过。构建的ECharts大chunk提示不作为编译失败；图表保持懒加载。

Pass：Go statistics_srv聚焦Usage/RangeCache/CacheHit测试，包括7类范围边界、去重来源、生命周期不受筛选、大整数/NULL/真零、权限与错误语义；共享算法与import格式化已完成。Server最终构建通过，无数据库迁移、新依赖或上报协议改动。

Pass：实际loopback Server托管业务静态产物，原合成SQLite。概览5792.6万/$24.01/18保持；热力图365格与宽度自适应保留；Codex范围/模型缓存25.0%及63.3%、会话生命周期90.0%来自实际查询，其他平台/混合范围未知。陈旧账号无当前可信曲线/预测，历史仍可查。订阅备注实际保存并重载读回，随后恢复原备注；仅合成库修订号增加。八页桌面与390px布局检查、最终浏览器console error/warning均为空。

Not Run：本轮没有重新签发管理码、撤销真实客户端或单独验证匿名配对界面；沿用已有授权/client行为测试，配对表单只有视觉调整。原生App/真实Home、三机、MySQL、生产HTTPS/部署/发布、Docker和CI均未执行。业务浏览器与SQLite证明不能推广到这些环境。

## 交付自查与结论

已审查本轮diff：原admin权限、Cookie/Origin/CSRF与collector隔离保持；普通文本/richText输出继续转义，官方来源URL仍精确限制，表单修订CAS与查询预算保留。新增缓存字段不接收客户端最终值，不包含原始JSONL、正文、API key/cookie或认证日志；无服务端网页抓取、新匿名接口或本机后台进程。预览只监听loopback，使用合成库与既有授权。

没有发现待修P0/P1/P2。Execution出口为已批准设计的真实业务接入及聚焦开发证据；正式整体验收留Master。提交推送收尾按约定不重复测试。原始截图/日志只在ignored `.artifacts/`，不提交。

final result: passed（本轮开发范围）

---

以下为独立设计阶段及此前业务/原生UI的历史QA；各阶段结论按当时范围理解。

# TOO-517 全站 Storybook 设计 QA（2026-10-03）

本轮仅交付独立、可交互的合成设计稿，生产页面未替换。最新用户决定为视觉依据：概览补齐现有信息、热力图置顶、模型折线、删除页标题副标题；节奏与历史按Mac的四指标/四类曲线/推算与对比组织，普通采样实线连接缺口两端，隐藏密集圆点。用户已否决的阶梯和断线不再作为目标。

## 对照证据

- source visual truth path：`.artifacts/multi-machine/design-review-20261002/candidate-1.png`，此前已选方案1的视觉语言。结构位置、平台和数据及副标题由上面的用户修正覆盖，不进行旧图逐像素复制。
- implementation screenshot path：`.artifacts/multi-machine/storybook-20261003/overview-viewport.png`与`overview-final.png`；账号/模型/来源/项目/会话/价目/设备/登录分别为同目录`*-final.png`。
- full-view comparison evidence：`comparison-final.png`，左方案1、右Storybook，同一对照图；focused comparison：`comparison-header.png`。后续概览完整内容在`overview-final.png`，密集采样及Mac组织在`curve-design.png`、`pace-mac-layout-final.png`，均实际Chrome渲染。
- viewport与归一：方案1为1487×1058像素，Chrome CSS1487×1058，概览截图1472×1047；将原图缩至1472×1047后同图比较，约1%桥接密度差不作为字体误差。独立节奏组件最终CDP按标签页1487×1058、deviceScaleFactor1检查，内容截图1200×668；390×844窄屏为1倍像素，文档375px含滚动条。临时视口均已清除。
- state：浅色、近30日、Asia/Shanghai；全部统计、账号、ID、价格与设备码为合成样本。参考既有18085概览的内容结构、Mac的QuotaPaceViews/Presentation源代码及已读的公开GitHub项目。Mac源码仅为组织/绘制参考，不宣称启动或验收了原生App。

## Findings 与迭代

1. [P2] 首轮窄屏账号Grid被横向账号列表的最小内容宽度撑到976px。修正：Grid子Card `min-width:0`，列表仅在自身滚动；修复后390px文档375px，见`mobile-accounts.png`与`stale-final-mobile.png`。
2. [P2] 首轮390px工具条刷新按钮孤立换行，固定星期的背景只覆盖字高。修正：窄屏按钮/Segmented间距与内边距收紧，星期列背景覆盖整格高度。修复后`mobile-overview-final.png`保留365格、最小14px和局部滚动，无全页溢出。
3. [P2] 订阅弹窗与Credits用了同一账号React key；打开表单时捕获重复key错误。修正为`subscription:`/`credits:`前缀，保存与同邮箱另一账号隔离、打开取消复查后不再产生该错误。旧错误在原证据中保留。
4. 首轮概览信息过少由用户指出，按现有产品补齐客户端/模型明细与分别独立的分布图、工具技能、年度统计、质量证据与下钻；副标题统一删除。`overview-final.png`保留完整内容，未把图稿数字冒充真实统计。
5. 采样方案先由圆点改阶梯，再按用户否决改成连接已有两端的普通实线；最后按Mac重排为四项指标、四类同图曲线、保守推算与同进度对比、展开明细。`dense-pace-step-rejected.png`只作被否决历史；最终为`curve-design.png`与`pace-mac-layout-mobile.png`。NULL样本仍存在，连接线不新增观测或预测证据。模型用量的未知日仍保留断点。
6. 修改story名称期间捕获Storybook热更新瞬态`cannot render when not prepared`；稳定保存并重新打开后，最新账号/节奏/历史切换/陈旧和窄屏检查未捕获新console error。不能将第一次含旧错误的22条列表称为零错误；最终稳定检查另行记录。

## 必查视觉表面

- 字体：沿用系统sans serif/PingFang，页头23px、主数字26px、表格13px、辅助11–12px；数字等宽，长ID可复制/换行。聚焦对照与密集曲线图检查标题、表格、图例、坐标及预测文字的层次，无字体/字重阻塞项。
- 间距与结构：208px左栏、28px内容边距、18px区块间距，白色单层表面。热力图、摘要、趋势、明细、分布、工具完整且顺序明确。账号是账号列表+详情，记录仍是左右Splitter；窄屏逐级进入/返回，详情未改成抽屉。
- 颜色：AntD主题蓝、浅色背景与有限语义色；实际采样蓝实线、上一周期灰蓝实线、近四周期紫虚线、理想节奏灰点线。陈旧/冲突主值未知，说明图标醒目，预测暂停不显示假正常。
- 图片与图标：没有摄影、头像或装饰插画需求；品牌/功能使用官方AntD图标，ECharts图表与日历为数据可视化。旧图虚构第四平台、平台冒充设备来源及头像未复制，不自画装饰资产替代真实资产。
- 文案：中文/万亿单位/两位金额，与原始ID和原观测分开。当前参考、历史成本与上报费用不混同；业务说明从长Alert改到图标/明细，页标题副标题已删。连线含义在说明中可查，缺失证据继续保留未知。

## 开发验证与交互

- 九页、八个关键状态、五个公共组件共22条均实际浏览器渲染并有索引/截图。类型、聚焦AntD lint（0问题）、最终Storybook静态构建通过；新增依赖后原Web生产构建通过。构建存在设计包/ECharts大chunk提示，不是编译失败，设计稿不进入业务入口。
- 实际操作：日期7/30/90与自定义浮层、年度证据、模型/金额切换、订阅保存/取消与同邮箱不同账号、Credits及节奏说明、项目→会话→返回、设备改名与模拟配对、价目模型/套餐切换；最新Mac结构的展开明细及上一周期选择通过键盘读回。配对只使用不能认证真实服务的示例码。
- 390px九页及项目内会话详情已检查；最新完整概览、密集节奏与陈旧账号额外检查，无全页横向溢出。宽表/全年日历只在自己的容器滚动。原始证据在ignored`.artifacts/multi-machine/storybook-20261003/`。
- 不新增镜像布局的单元测试，不把合成设计/构建当真实API、MySQL、三机或生产验收。业务产物继续沿用原18085构建，原生App未启动，采集/上报/鉴权未改。

## 交付自查与边界

独立Storybook只绑定127.0.0.1:6007，Vite配置不继承`/api`代理；设计数据只在内存，React文本和ECharts richText，不读取真实Home/JSONL/凭据，不创建实际管理会话或匿名业务接口。依赖锁定于标准npm registry，安装阶段审计0漏洞；源码和提交证据只含合成信息。

最新对照没有待修P0/P1/P2。生产替换与用户最终设计确认、真实API/MySQL/三机验收仍待后续，归Master，不与本卡的设计可评审出口混同。提交推送收尾按约定不重复测试。

final result: passed

---

以下为原业务Web及原生UI的历史QA，不能代替本轮设计稿结论。

# TOO-516 热力图置顶与自适应补充（2026-10-02）

最新用户要求为本轮视觉依据：热力图在筛选下方、指标和趋势上方；拉宽时正方格同步增大并铺满内容区。该要求覆盖下方方案1历史记录的底部位置与固定14px上限；旧原图不再作为这两项的验收依据。

- 实现：等宽 Grid 列随容器增长，最小14px、3px间距、七行与完整365日期保持；末列月份右对齐，避免标签产生额外横向溢出。
- 1280×914 Chrome：方格14.828×14.828px，日历963px，滚动容器 clientWidth/scrollWidth 均969px，无额外横向滚动。热力图、摘要、趋势依次可见。
- 默认1971×914 Chrome：方格24.469×24.469px，日历1474px，容器 clientWidth/scrollWidth 均1480px。活动/摘要/趋势顶部约122/431/562px；完整365格，横向填满。
- 390×844 Chrome：方格14×14px，365格保留，容器315px、内容925px；仅日历局部滚动，documentWidth375px（15px滚动条），无页面横向溢出。End键访问最新日期2026-10-02，星期固定左侧；方向键和未知/零语义有原聚焦行为证据。
- 年度范围仍独立于上方统计日期，年度指标来自Server；摘要5792.6万/$24.01/18保持。捕获console error为空。

截图在ignored `.artifacts/multi-machine/heatmap-top-20261002/`：`overview-wide-final.png`、`overview-1280-final.png`、`overview-mobile-final.png`、`heatmap-mobile-end.png`。默认视口已恢复，预览托管最终构建并复用原合成SQLite和浏览器授权。

开发检查：Overview/ActivityHeatmap两个文件9场景通过；末月边缘修正后ActivityHeatmap的4场景再次通过；最终类型/生产构建与AntD lint通过（0问题）。仅Web几何与显示顺序调整，未改认证/DTO/Go/数据库/本机采集；安全自查确认React文本转义和既有权限边界保持。没有新增待修P0/P1/P2；真实MySQL/三机/生产仍待Master，提交推送收尾不重复测试。

final result: passed

---

以下为27b2f4e阶段的历史视觉记录；底部位置和固定14px选择已由上方用户修正覆盖。

# TOO-516 方案 1 视觉验收（本机合成预览）

- source visual truth path: `.artifacts/multi-machine/design-review-20261002/candidate-1.png`
- implementation screenshot path: `.artifacts/multi-machine/design-review-20261002/implementation-final.png`
- viewport: Chrome，1487 × 1058 CSS px，浅色，概览，近 30 日，Asia/Shanghai，已授权管理浏览器。
- 像素：原图 1487 × 1058；浏览器截图 1472 × 1047。比较将原图归一为 1472 × 1047；浏览器截图保留原像素。浏览器截图桥接密度与 CSS 存在约 1% 差异，不能把这点当成排版错误。
- full-view comparison evidence: `comparison-final.png`，左原图、右实现，位于同一证据图。
- 状态差异：原图是生成示例；实现使用原合成 SQLite 的真实 HTTP 数据，因此只有三个平台、真实陈旧状态和未观测缺口。总览 API 没有 Provider 日桶，使用真实汇总柱，模型页另有真实模型日桶堆叠。图稿的第四平台/假正常不作为视觉要求。

**Findings**

- [P1] 首轮全年热力图低于首屏。实现 supporting 区 338px，活动区顶部 y=1004；图稿活动区顶部约 850。原因是重复状态行、表格每行金额小计及年度展开条占用纵向空间。修复：表格部分成本改成同行信息提示；范围质量与年度统计使用可点击 Popover；缩小摘要内边距；方格上限 16px。
- [P2] 首轮明细/采集区密度不齐。平台表额外 Tabs 与两行金额使两块被拉高。修复：减小顶部空间、保持行内金额状态与可查提示，按真实设备显示三行预览。

**Required Fidelity Surfaces**

- 字体：系统中文 sans serif、22px 页头、30px 摘要，层次成立；最终聚焦对照确认表格 13px 与辅助 12px 可读。
- 间距：216px 侧栏、24px 内容边距、16px 区块间距；最终摘要 y=122、趋势 y=253、明细 y=556、年度 y=830，年度底部约1049，进入1058px首屏。
- 颜色：浅蓝侧栏、白色内容、蓝色强调、低对比边界；真实陈旧用有限警示色，不能改成图稿虚构绿色状态。
- 图片/图标：目标无产品摄影/插画资产；品牌柱图及功能图标使用同系列 AntD 官方图标，图表/日历是数据可视化，无自画装饰替代图片。
- 文案：业务元数据来自接口；修正图稿的 ChatGPT 独立平台与平台充当来源，保留 unknown/partial/采集截至语义。

**Comparison History**

1. `comparison-1.png`：首轮 P1/P2 为热力图低于首屏、两行金额状态和多余统计条。
2. `comparison-2.png`：行内金额、Popover 和 16px 方格已落实；热力图仍从 y=922 开始，P1 仍阻塞。继续把明细切换放入标题、减小摘要/图表空间。
3. `comparison-3.png`：两块明细已对齐，活动 y=864；年度 246px 导致底部仍被裁，P2 仍阻塞。继续将质量证据放到趋势标题，全年固定14px，减小活动头和内边距。
4. `comparison-final.png`：所有首轮 P1/P2 已修复，全年完整 365 个日期、月/星期/图例在首屏内；聚焦 `comparison-final-header.png`、`comparison-final-tables.png`、`comparison-final-heatmap.png` 确认字体、基线、行高及日历几何。补齐首个可容纳的部分月份标签，短月片段不挤压下一月；侧栏父级选择层级恢复。没有新增待修 P0/P1/P2。

**Implementation Checklist**

- [x] 修复后同尺寸桌面截图与密度复查。
- [x] 聚焦侧栏/指标、明细/热力图的同图对照。
- [x] 模型、账号、价目、设备、项目会话主操作与 390px 窄屏检查。
- [x] 控制台与失败/未知/零语义复查。

**交互与响应式证据**

- 自定义日期实际打开双月日历，范围质量与年度统计 Popover 可见；模型真实日堆叠与成本切换、价目搜索/用量跳转可操作。旧 `/quota?view=usage&provider=codex&model=gpt-synthetic` 自动转入模型页并保留筛选。
- 账号原 ID、Credits 和订阅编辑按同邮箱两个账号分别读回；过期/陈旧窗主摘要“当前未知”，进度条为0个，原值和原时间保留。仅打开/取消编辑，没有修改订阅或签发新码。
- 项目/会话桌面左右两侧约410/796px，无详情弹窗；窄屏会话详情仍保留隐藏列表 DOM，缓存90.0%、输入1000/缓存900与原Session ID可读。
- Chrome 390×844：概览、账号、模型、价目、来源以及导航/会话详情可用，文档宽度375（15px滚动条），无页面横向溢出；宽表/全年日历在自己的容器内滚动。`mobile-review.png` 与 `session-detail-mobile.png` 为截图。
- Chrome captured console error 列表为空。不存在后端/数据库/原生App的新增实现证明；MySQL/三机/生产/CI仍Not Run。

**可接受差异与后续细节**

- 原图四平台堆叠、平台Logo和假新鲜来源不复制。使用三个既有平台、真实汇总柱与真实设备来源；模型日桶在模型页可查。不替换或猜测数据以接近图稿。
- 14px全年格子在宽桌面留出右侧空间，目标是与既有掘金几何对齐且不拉成灰墙；内容、月份和星期靠左对齐。该差异为产品明确选择。
- 图稿未定义失败/未知/可编辑订阅等状态，实现沿用现有语义。标准图标复用AntD，不增加头像或未支持设置功能。
- P3：未来有真实 Provider 日桶时可增加首屏平台堆叠；当前不能比例分摊。不是本次待修项。

final result: passed


---

以下保留此前原生页面的历史 QA，结论不代表本次 Web 验收。

# TOO-418 汇总页 Design QA

## 对照输入

- 视觉参考：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-aa78d4c3-74c6-4239-808e-d32e9a671812.png`
- 位置参考：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-9475f105-8ac6-4c8e-adec-5bf944f24756.png`
- 趋势反馈：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-6ea95c46-d854-4283-a738-14ab650da00a.png`
- 顶部信息精简参考：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-66994c12-8d6f-424f-a750-9876bf2be2c1.png`
- 堆叠柱边界反馈：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-328db841-986b-4727-b999-c5b7a075131c.png`
- 7 天柱图密度反馈：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-ffd4db5b-4b7e-45fe-be7f-75e8444f78ae.png`
- 年度指标条参考：`/var/folders/j1/blrv77y956q8d747sb8pqfvm0000gp/T/codex-clipboard-6ed1c953-353d-448a-8a7d-d2f20fe6f824.png`
- 实现截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-top.png`
- 实现截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-cards.png`
- 堆叠趋势截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-stacked.png`
- 端点修复截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-stacked-7d.png`
- 端点修复截图：`.artifacts/too-418-dashboard-summary/dashboard-summary-stacked-30d.png`
- 同屏对照：`.artifacts/too-418-dashboard-summary/reference-comparison.png`

## 验收环境

- 原生 macOS Development App，窗口 `1440 × 984 pt`
- 状态：汇总页、今天与近 7 天、全部客户端、Token 与费用口径
- 数据：真实 Codex Home；使用私有 runtime 和线上数据库副本，未修改生产数据库

## 对照记录

1. 初版：年度热力图位于 KPI 之前；趋势图遗漏原概览的数据点层；构成条偏细；分布图缺少明确的 pointer preview。
2. 修正：页面顺序改为 `KPI → 年度热力图 → Token 趋势 → 三张分布卡 → 额度`；覆盖度保留在 KPI 卡内，不再重复显示页级日期和 partial 提示；趋势图恢复既有 `AreaMark + LineMark + PointMark` 语义。
3. 修正：模型构成条增至 `28 pt`，按客户端堆叠柱按时间范围使用 `20–64 pt` 自适应固定宽度，其中近 7 天为 `64 pt`，柱体约占每日槽位四成；热力图、趋势图、构成条及两个环图均保留独立悬停命中层与详情反馈。
4. 修正：连续 `Date` 轴的自动 domain 以首末采样点为边界，固定宽度柱因以采样点居中而被裁掉一半。现将柱宽与对称绘图留白绑定，近 7 天与近 30 天的首末柱均完整位于绘图区内。
5. 修正：近 7 天柱宽从 `34 pt` 增至 `64 pt`，对称绘图留白同步从 `21 pt` 增至 `36 pt`；实机对照中 7 根柱的视觉密度更均衡，日期标签、图例与首末边界仍正常，近 30 天密度不变。
6. 修正：X 轴不再使用与真实采样点脱节的自动刻度，改为从趋势数据中选取首尾必含的真实 bucket，并用显式居中锚点渲染日期。实机检查近 7 天的 7 个日期逐柱对齐且显示末日 `9/4`；近 30 天保留 7 个均匀日期，首日 `8/6`、末日 `9/4` 与中间刻度均对齐。
7. 修正：Cursor Dashboard 成本改为逐事件定价。可定价事件形成 API 等价成本已知小计，并明确标记“部分可估算”；Cursor 官方上报费用只在 Cursor 客户端行以“实际上报”独立展示，不进入顶部 API 等价成本、工具费用分布或模型费用分布。
8. 修正：年度热力图上方增加近 365 天 Token、峰值日 Token、活跃天数、当前连续天数和最长连续天数；五列使用等权视觉层级和细分隔线，并随“全部客户端 / Codex / Cursor / Grok”范围复用同一年度数据口径。窄窗口回落为自适应网格，避免标签裁切。
9. 最终同屏检查：三张参考卡的层级、左右布局、环图与图例关系、Token/费用切换、横向构成条以及年度指标条均已复刻；颜色、圆角和材质继续遵循 Codex Pulse 现有原生设计语言。

## 结论

通过。参考图与实现中的实时数值、客户端数量和模型数量不同属于数据差异；未发现裁切、重叠、不可读标签或错误的卡片顺序。
