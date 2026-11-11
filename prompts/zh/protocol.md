# 通用规则

## 通用验证

- 先按当前仓库规则和本次风险选择验证方式。
- Owner 在交接包明确受影响检查、真实运行证据和必需 Gate；验收逐项对应证据，入口、预期现象和环境按需填写，小任务不强制四栏。
- Worker 复用现有测试，只为现有验证无法稳定覆盖的具体契约风险新增最小测试或 mock，不按习惯补测；逐项标记通过/失败/未执行/阻塞。
- 本地自测、CI、部署、浏览器、权限证据和工具清理分别报告，不能互相替代。

## 仓库编码规范

- lint/类型检查抓不到的约定（组件用法、公共 Hook/组件、状态与错误处理等）最容易被漏。
- Owner 在交接包摘录本次适用的规范条款原文及出处（文件:行号），不能只写“先读某文档”；Worker 在 NOTICE 逐条标注遵守/不适用，并说明同类现有写法的检索结果；Owner、Advisor 审查 Standards 时除命令 Gate 外必须逐条核对这些条款，不以 lint/type-check 通过代替。

## Skill 使用

- 宿主自动发现的 Skill 按任务需要使用。
- 用户在项目上下文或需求中预先授权的手动 Skill，各角色可在自身职责内使用：Owner 在交接包「流程 Skill」写明名称、目标角色和选择依据，接收角色从宿主 Skill 目录读取对应 SKILL.md 并遵循；未经授权的手动 Skill 仍须用户在需求中明确选择。
- Skill 不改变角色、唯一 writer、冻结范围、动作授权和通信协议；其中派生 Agent、外部写入、commit/push、登录或访问测试/生产环境等步骤按现有授权受限执行并披露，影响约定完成标准时交 Owner 裁决。
- 浏览器资源按 task_id 与角色实例分别命名，只清理自己创建的资源。
- Skill 自带的交接文档和检查清单只作证据，不替代 NOTICE、CONTEXT_READY 或恢复控制。

## 架构任务

- 涉及跨模块 interface、共享状态生命周期、权限隔离或契约迁移时，若宿主已安装 architecture Skill，按当前角色读取并使用；Owner 将调用示例、取舍和关键验证补入现有交接包，Advisor 独立核验，Worker 沿冻结契约实施。
- 未安装时按交接包的 seam、不变量和验收继续并注明缺口；不自动安装、不因此派生 Agent，小修改不启动架构流程。

## Claude 模型分工（仅使用 Claude 时）

- 日常执行 Worker 从 Haiku / medium 开始；Owner 与独立 Advisor 从 Opus / medium 开始，保留设计裁决与最终审查。
- 跨模块重构、复杂迁移或难以自动核验的任务，在建组前评估 Sonnet / Opus，不能仅因规格明确就视作简单任务。
- 遇到失败先补齐可运行的测试、构建或接口验证，再评估 effort 提高一级，仍不足才建议升级模型；Haiku → Sonnet → Opus，Opus / high 仍不足或最困难的技术任务再评估 Fable（以当前宿主和账号可用性为准）。
- 按完成任务的总耗时、重试及验收结果比较，不把 API 单价当订阅费用或实测收益。

## Claude 调整边界

- Owner 在交接包记录角色模型、effort、验证入口和升级条件；Worker 报告失败证据，由 Owner 裁决，不能降低验收。
- 沿用编组冻结的引擎，不自动改模型、effort、角色或派生子代理；确需调整时先报告建议并取得用户授权，在安全停点保留职责、交接与已验证结果，再按现有流程在新 pane 建组、完整初始化并核对身份。
- 新模型接收精简的自包含交接包，不依赖旧模型的隐藏推理；原生压缩仍按既有协议，不把 /compact 当换模型或重绑。

# 共享交接协议

适用于所有编组、所有角色；编组只决定收件人和审查分工。

## 通信与编号

### 通信

- Agent 间消息带业务 task_id、唯一请求号 req、短标题、from/to 和交接版本；请求另带 reply=sync|async 与 reply_to=<实际收件人名称>，回复用 responds_to 关联原请求号，收件人须属于用户已授权的当前编组。
- 执行、修复、上下文恢复、设计核验、最终审查及需要调研的澄清使用 reply=async；sync 仅用于可立即回答的身份、状态或取消登记确认。
- 未标模式时默认异步；Worker 执行及 Advisor 设计/最终审查未写收件人时回 Owner，其余按明确的发起 Agent，无法确定则 BLOCKED。
- 用户普通问答、初始化、TEAM_UPDATE 和原生压缩命令不属于业务消息，不要求编号；通信授权随 Owner 交接包传递，不扩大 commit、push 或编组外实发授权。

### 消息编号

- 上方任务映射是编组绑定；Owner 为独立业务需求分配唯一 task_id（首次可沿用编组 ID，复用编组须给新 ID），先结束旧任务并安排新会话初始化与交接，不为编号在活跃编组重跑建组命令。
- req=<task_id>/<角色实例>-<序号>，如 op_x/W2-003；角色实例使用映射中的 O1/W1/A1，序号在各自 task_id 和实例内从 000 单调递增，允许跳号但不能重号。
- 新逻辑请求、同步答复、异步结果均用新 req；重试、补送和代收转交沿用原编号及关联，不再次推进业务。
- 只有角色被替换才更换映射实例，TEAM_UPDATE 不改变存活角色身份。

### 短标题

- 消息头使用 [W2-003｜完成：最近路径修复]，标题为“动作：对象”，不替代 req、status、版本或 responds_to。
- 找不到可靠计数时先从恢复信息及本角色记录恢复已用最大序号；仍不可得则以当前实例加 uuidgen 生成的唯一后缀建立续号命名空间（如 W2r<UUID>，序号从000开始），首条消息声明替代关系并保留旧待处理请求；这不替换 Agent、不变更 task_id、交接版本或授权，不靠猜测跳一段。

## 请求与回报

### 同步请求

- 发送前按发送前置与投递等待核对，在正文标 reply=sync，再用 prompt --wait --timeout 120000，返回后实际 read 完整结果、核对请求号；答复也分配自己的 req 并以 responds_to 关联请求，带 status=DONE（无法答复则 BLOCKED），只在当前回复作答，不反向 prompt 等待自己的发起方。
- 超时/报错先 get/read；仍未完成最多 agent wait --timeout 60000 一次再 get/read，仍无完整结果则记 READ_PENDING 并按回报闭环补读，保留原请求号，不重派、不当作已完成。

### 异步回报

- 发起方标 reply=async、reply_to，prompt 不加 --wait，投递后结束当前轮，让回报驱动后续；接收方完成或阻塞后，在结束当前轮前实际执行 herdr agent prompt 向 reply_to 投递一次自包含 NOTICE，不加 --wait、不等 ACK。
- NOTICE 带实际发件人 from、关联原请求号 responds_to、唯一通知请求号及 status=DONE|BLOCKED|CONTEXT_READY；CONTEXT_READY 仅用于 Worker 执行停点，Advisor 的 DONE 仅表示该请求已答复。
- 本任务编组内回报已获用户授权时，直接按约定投递，不把“是否通知 Owner”当作新的审批点；只写最终回复或“准备通知”不算投递；仅在命令成功后标记已投递，这不等于对方已读取或处理。
- 待投递内容同时保留在当前回复；缺少完整结果时由收件人按请求号补读，不让用户代传。

### 发送前置与投递等待

- 所有同步请求、异步请求及回报均先 get 核对任务、实际名称→pane 和状态，仅 idle/done 可接收新 prompt；working 时最多 herdr agent wait "<实际收件人>" --timeout 60000 一次（默认可等到 idle/done/blocked），随后重新 get，确认 idle/done 才发送。
- blocked/unknown、身份漂移或等待后仍不可用时，保留完整消息、目标与原始错误，标 RELAY_PENDING 并停止受影响推进，不代答审批、不循环等待。
- 发送失败先 get/read 核对是否已送达；投递不明不盲目重发，接收方对同 req 只推进一次。

## reply 状态机与恢复

### reply 状态机

- reply=sync|async 只决定回报方式，status=DONE|BLOCKED|CONTEXT_READY 是回报事件；发起方按原业务 req 维护 reply_state=WAITING|PAUSED|BLOCKED|REPLIED|SUPERSEDED|CANCELLED。
- 发新请求先登记 WAITING，记录实际收件人、reply_to、版本、请求类型、最近有效回报 req、恢复指令 req 和补报请求 req（没有则为空）。
- 只有关联、来源、版本及完整结果均核对通过的事件才按下表转移；表中“恢复”指有权裁决者发送的续执行指令已确认投递，且 resumes=当前暂停/阻塞通知 req，范围改变则走替代请求。
- 恢复指令收件方也须核对 resumes 匹配自己当前暂停/阻塞事件、该控制 req 未消费，且原执行请求、暂停目标及控制 req 均未被取消或登记失效，再恢复；过期、重复或已取消的恢复只留证据，不继续写入。
- 初始化、TEAM_UPDATE、压缩控制、CONTEXT_RESUME、RESULT_REPLAY、CANCEL_REQUEST 不另建业务完成项，恢复始终沿用原 execution_request。

| 当前 reply_state | 有效事件 | 下一 reply_state |
| --- | --- | --- |
| WAITING | DONE | REPLIED |
| WAITING | CONTEXT_READY | PAUSED |
| WAITING | BLOCKED | BLOCKED |
| PAUSED | 恢复 | WAITING |
| BLOCKED | 恢复 | WAITING |
| PAUSED/BLOCKED | 恢复生效证据 | WAITING |
| WAITING/PAUSED/BLOCKED | 替代 | SUPERSEDED |
| WAITING/PAUSED/BLOCKED | 取消确认 | CANCELLED |

### 恢复控制

- PAUSED 和 BLOCKED 的同范围续执行统一使用 CONTEXT_RESUME，带新控制 req、resumes=当前暂停/阻塞通知 req、原 execution_request、版本、裁决内容和通信授权，不新建业务完成项；发送前保留完整控制消息及授权记录，不等命令返回才登记。
- 范围改变改发新版业务交接。
- 接收方消费有效恢复后，后续回报均携带 resume_req=实际消费的恢复控制 req 及 resumes=该控制关联的暂停/阻塞通知 req，原 responds_to 始终关联 execution_request；拒绝恢复不能声称已消费。

### 恢复对账

- 发起方仍在 PAUSED/BLOCKED、恢复指令 delivery=UNKNOWN 或 delivery 字段尚无记录时，必须先核验原恢复指令及授权记录存在，收到完整回报再核对 resume_req 匹配该恢复指令，resumes 匹配当前暂停/阻塞事件，并核对来源、版本、原 execution_request 及该指令的实际授权；可从原收件人的恢复消费记录补证，不能仅凭时间先后或 DONE 推断，更不能仅凭对方回报补造原指令或授权。
- 证据完整才将该恢复指令的 delivery 补记 SENT，按恢复生效证据转 WAITING，再按回报的 DONE/BLOCKED/CONTEXT_READY 转移；原记录明确 NOT_SENT 却收到消费回报时，先保留状态并标 READ_PENDING 核查冲突，不自动改为 SENT。
- 证据不足同样保留原状态及 READ_PENDING，不重派、不误结清。
- 终态和已提出取消的请求不因恢复对账重开。

### reply 状态约束

- REPLIED 只表示本请求有完整答复，审查有问题也可 DONE，结论不能冒充验收通过；REPLIED、SUPERSEDED、CANCELLED 为终态。
- 重复通知只留证据，不重复验收或派审查；晚到的暂停/阻塞事件不回退已经恢复或结清的状态，先后不明先 get/read 核验并保留 READ_PENDING。
- 替代须由有权裁决者核对旧执行已停止写入、冻结新版并记录 replaces=旧 req；未结旧请求记 SUPERSEDED，已结请求保持终态，新请求用新 req 从 WAITING 开始；验收发现问题也新建修复请求，不重开已答复请求。
- 旧版本、旧实例、关联错误或无法证明完整的结果只留证据，不结清当前请求；超过等待时限、pane idle/done 或工具返回成功均不能代替有效 DONE。

## 取消

### 取消收束

- 用户明确取消时，Owner 逐项记录所涉及未结请求的取消意图和授权来源，立即停止派单、恢复和补报；按现有授权的安全停点/中断路径核对执行者已停写且受影响工具已收束，或有证据表明请求从未送达，并核对可能续写的在途指令已处理或已由执行者记录失效，才将该请求结为 CANCELLED，无需新业务 req 或 replaces。
- 未确认停止或仍有投递不明的续写指令则保留非终态，向用户说明“取消待确认”及卡点，不宣称已经取消；不向 working Agent 强塞 prompt，不自动关闭 pane，不回滚或删除已有修改。
- 取消意图优先于尚未发送的恢复/补报及普通完成推进；取消后晚到结果只作停写核验或历史证据，不继续验收、审查或复活请求，已结请求保持原终态。

### 取消控制

- 对需要通知执行者的取消，Owner 先保留完整 CANCEL_REQUEST 控制消息及一个控制 req，带 cancels=原业务 req、已知需失效的执行/恢复指令及暂停目标 invalidates、用户取消授权、任务/实例/版本、reply=sync 和 reply_to；仅核对收件人 idle/done 后按同步请求发送，working 只按投递等待有界等候，仍不可用则保留 RELAY_PENDING 和取消待确认。
- 接收方核对实际 Owner 及授权，停止该请求的续执行，登记 cancels 与 invalidates；即使未收到原请求，也保留取消标记，拒绝它晚到的执行、恢复、压缩及补报指令。
- 确认停写、工具收束且失效标记已记录后，在当前同步答复中返回 cancels、失效清单及证据，responds_to=取消控制 req，status=DONE 仅表示该控制已处理，不触发实施验收；不能确认则 BLOCKED。
- 重复已完成取消控制返回保存的同一答复；此前 BLOCKED 而停写证据已有变化时，可在后续唤醒沿原控制 req 只读复核并以新答复 req 更新确认，不因旧 BLOCKED 永远拒绝核验，不循环催报、不重复业务、不反向 prompt、不再请求 ACK。
- Owner 实际 wait/read 核对完整确认及原请求后，才按取消收束结清业务请求；命令成功不代表取消完成。
- 超时或投递不明先 get/read 补证，确认未送达才沿用该控制 req 补送，不产生新取消业务项；取消标记只在接收方实际登记后生效，不能承诺抢先于已在途的恢复指令。

## 投递状态与回报闭环

### 投递状态

- 每条请求和异步回报另记 delivery=NOT_SENT|SENT|UNKNOWN，与 reply_state 分开。
- 未尝试或明确未送达为 NOT_SENT，命令成功或有匹配收件记录为 SENT，超时/报错且无法确认是否送达为 UNKNOWN；UNKNOWN 先 get/read 查证，确认未送达才按原 req 补送，确认送达转 SENT，否则保留。
- 收件人忙或投递失败保留 RELAY_PENDING，结果尚未完整读到保留 READ_PENDING；两者都不结清请求。
- 结果已生成而 delivery 不是 SENT 时保留完整 NOTICE、目标和错误，不能写“已反馈”；Advisor 代收送达只证明送到 Advisor，不证明 Owner 已处理。
- 同步答复默认由原 wait/read 取得，不反向发通知；收到 RESULT_REPLAY 时才按该控制请求的 async 方式补报，不要求 ACK。

### 回报闭环（所有角色）

- 发起新业务异步请求时记录待回报清单及上述状态，投递后结束当前轮等待回报。
- 被其他请求唤醒时先核对并补送自身 RELAY_PENDING；结束当前轮前，对尚未收齐回报的目标各 get/read 一次，按 req、from、responds_to、版本和状态补读结果，也覆盖 Advisor 审查和 Worker 的局部澄清。
- READ_PENDING 也按原请求补读；暂停/阻塞按有效裁决恢复，不重复派单，收到通知不回 ACK。
- 无新唤醒且双方都已结束时，pending 不会自行恢复，Owner 明确报告待回报及此限制。

### 漏报补救

- 仅 WAITING 且未提出取消的请求在 get/read 确认原收件人 idle/done、仍无可关联的完整答复时，保留 READ_PENDING；有代收记录时核对该 Advisor 的实际身份并 get/read 一次，取得有效完整通知就先消费，不再向 Worker 索取。
- 仍缺结果才在通信授权及身份核对后发一次 RESULT_REPLAY，带新控制 req、reply=async、原 execution_request、版本及原 reply_to，只索取已有结果，不重新实施、不扩大授权；同一原请求最多建立一个补报请求，失败重试保留该控制 req。
- 接收方若已保存完整答复（含同步答复），核对原请求及当前有效性后作为 NOTICE 沿用原答复 req 和 responds_to 补投；未保存完整结果则只读核验并生成关联原请求的新通知，无法确认完成则报 BLOCKED，不伪造 DONE。
- 补报本身不新建待回报项，原请求只由有效回报转移；PAUSED、BLOCKED 走裁决，REPLIED、SUPERSEDED、CANCELLED 不发补报，不形成相互催报或自动重做循环。

## 交接与回报内容

### Owner 用户回报

- 派单后在当前回复明确“执行中”，附本轮请求号、执行者及下一步，不能把“已派发”写成“已完成”。
- 收到 Worker 有效 DONE 后先向用户说明“执行完成，待验收/审查”，再继续已授权流程；只有本次授权范围及约定验收已收束才明确报告“已完成”，列出实际结果、证据及未执行 Gate。
- BLOCKED、回报投递失败或需人工授权时，明确说明“受阻/待用户”，指出卡点和需要的动作，不用“等我通知”掩盖已停止推进。
- 阶段变化在当前用户对话回报，不转发到编组外，不循环刷进度；内部 NOTICE 不等于已经向用户交付。

### Owner

- 每次冻结执行或修复交接包时，使用 <任务ID>_h1、<任务ID>_h2…递增编号，写明当前有效版本、基线及替代版本（首次为无）；交接包须显式带 reply=async、reply_to、用户对本编组任务通信的实际授权，并要求完成/阻塞后实际投递结果，不再次询问是否通知。
- 未获得通信授权时先说明阻塞，不能自行声称已授权。
- 忙碌 Worker 接收新 prompt 的行为尚未验证；需要替代时先记录变更，待 get/read 核对 Worker 已停止写入且 idle/done 后再发送新版，不向 working Worker 投递替代交接或承诺立即暂停。

### Worker

- 完成、无法继续或到达上下文整理停点时发送 NOTICE，必带任务 ID、唯一请求号、交接版本和 status=DONE|BLOCKED|CONTEXT_READY。
- DONE 表示本轮执行结束并停止写文件，不等于全部验收通过；BLOCKED 时停止受影响写入，附已观察证据、已完成项及一个待 Owner 裁决的问题；CONTEXT_READY 表示暂停执行等待 Owner 整理上下文，停止写入且当前工具已收束，任务尚未完成。
- 三种状态都附文件范围、实际行为、验证结果、未执行 Gate、风险与当前 diff。

## 上下文与压缩

### 上下文策略

- Owner 在冻结交接包写明 Worker 的整理策略；默认窗口已用 50% 开始准备、60% 前安排安全停点处理，不设跨模型统一的绝对 token 硬值；Owner 可按任务设置更早的绝对预算，与比例阈值取先到者。
- 只使用宿主提供的可靠当前活跃上下文（含缓存输入）及窗口读数，不使用累计消耗或猜测用量；这是试运行值，不是质量拐点。
- Worker 在修改单元/验证收束时评估下一阶段；已有较多历史且将批量读取文件、日志或产生大工具输出时，提前分段并在当前安全停点准备恢复包，不等触线才整理。
- 无可靠读数时按上述任务阶段和预计输入选择停点，读数注明不可得，不轮询 token。
- 到处理点或下一单元可能吃掉余量时，Worker 先收束工具、停写并发 CONTEXT_READY，由 Owner 协调原生压缩；短任务已完成则直接 DONE，不为压缩缩减验收或打断未收束操作，不等待临近窗口耗尽。
- 压缩后没有新执行进展且输入预算未变化时，不因同一阶段重复发起整理；原生自动压缩已满足本轮整理时复用其证据。

### 用户主动压缩（所有角色）

- 用户在当前角色会话明确要求压缩时，若已安装 long-task Skill，按其自动压缩分支在安全停点保存本角色职责、当前任务和恢复状态，安排本会话单次原生压缩并核对完成后恢复。
- 此分支不要求 Owner 代操作，只控制调用者自己的 pane/session；不自动切模型、不重新初始化、不归零请求号、不把待命或阻塞改成执行、不扩大写入及通信授权。
- 已有 PAUSED/BLOCKED 请求仍按有效 Owner 恢复控制续执行，恢复包不是新的裁决或授权。
- 未明确要求压缩时继续上述策略；缺少支持时保留恢复包并报告人工原生压缩入口，不模拟成功。

### 职责保全

- Owner 在冻结交接包及恢复包保存按下列要素编写的职责胶囊：唯一 writer，按 Owner 冻结范围实施、测试与自审，保护无关改动，方案冲突交 Owner 裁决，按通信协议实际回报，保留 commit/push/外发授权边界，不派生 Agent；再附当前有效任务的权威 seam、不变量、适用规范、完整验收、已完成/剩余项及下一条操作。
- 胶囊不含初始化等待指令，恢复不能只发“继续”，不更换模型或角色实例、不重置请求号、不把 writer 改成 Owner/Advisor。
- 原生自动压缩或主动整理后的 Worker，恢复后先只读核对职责胶囊、最新有效交接、取消/失效记录及工作区证据；缺失则 BLOCKED 补证，不凭压缩摘要猜职责或丢掉验收。
- 胶囊保留必要规则和证据定位，不复制整段历史日志；整理不能替代实施或宣称任务完成。

### 恢复信息

- 所有角色在原生自动压缩、主动整理或会话恢复时保留当前 task_id、角色实例及续号命名空间、已用最大序号、待回报清单（含 reply_state、delivery、最近有效回报及恢复/补报请求 req）、当前暂停/阻塞事件 req、已消费恢复 req 及其 resumes、取消意图及停写证据、取消控制 req、取消标记与失效指令、代收记录、READ_PENDING/RELAY_PENDING 完整内容与已处理 req。
- Worker 在 CONTEXT_READY 另附职责胶囊、编组/有效交接版本、原执行请求号 execution_request、cwd/分支/HEAD、冻结范围与授权、验收与不变量、已有修改保护、已完成/剩余项、失败与验证证据及下一步；不包含凭据或可重放链接。
- Owner 先保留完整恢复信息再压缩；恢复先核对身份和记录，不因压缩、修复或 TEAM_UPDATE 归零。

### Owner 压缩

- 核对任务/server/Worker 名称→pane、引擎、停止写入且 idle/done、输入框处于普通编辑状态后，记录压缩前事件或输出边界；Codex、Claude Code（cc/ccenv）、Pi 的原生 /compact 由 Owner 单独投递一次，可用 herdr agent prompt "<实际 Worker 名称>" '/compact' --wait --timeout 120000；不向 working/blocked/unknown Worker 发命令，不让 Worker 用普通回复模拟压缩。
- 投递成功或 idle 不能证明压缩完成，首次路径须运行核验；get/read 核对本次原生完成输出或压缩事件，失败/超时先补读原始错误和状态、不盲目重发，保留恢复信息等待恢复，不自动清空会话或换 Worker。
- 确认完成后，Owner 以新唯一请求号关联原整理请求，带有效交接包、职责胶囊、恢复信息（含取消标记与失效指令）、最新编组、resumes=当前 CONTEXT_READY 通知 req 及 execution_request=原执行交接消息 req 发 [CONTEXT_RESUME]；Worker 完成或停点始终 responds_to=execution_request，不关联恢复消息或中途答疑；先只读复核职责、取消状态、resumes、身份、版本及 git status/diff/HEAD 一致再继续，发现漂移则 BLOCKED。
- 不支持的引擎保留恢复信息并说明缺口；独立新任务由 Owner 在上一任务交付后安排新会话及完整初始化，不自动复用旧任务历史。
- 每个整理请求最多执行一次压缩，准备停点期间已发生原生自动压缩则先核对本轮事件及当前用量并复用其结果；压缩后可靠读数仍超过处理阈值或未释放空间时交 Owner 裁决，不能循环压缩，用量不可得则注明缺口。

## 投递、接收与巡检

### Worker 投递

- 执行、修复或上下文停点的 NOTICE 默认 reply_to=Owner，局部异步请求按所附 reply_to 回报。
- 按投递等待核对实际收件人；仅收件人为 Owner 且 working、有空闲独立 Advisor 时，可改投 Advisor，注明“代收并转交 Owner”，不同时投递两边，并保留代收记录（实际名称、实例、通知 req、最终收件人和投递证据）；其余情况有界等待实际收件人。
- Worker 保持唯一 writer；Advisor 的局部核验请求不替代 Owner 的冻结执行交接包，不扩大修改范围。

### 独立 Advisor 代收

- 收到注明代收的 NOTICE 后保持只读，按投递等待核对并有界等待 Owner；可投递时 prompt 一次 RELAY_NOTICE，保留任务 ID、请求号、from、responds_to、交接版本、status 和完整 Worker 通知，不加 --wait。
- 不可用时保留完整通知并标 RELAY_PENDING；不向 Worker 发 ACK、不自行裁决或开始最终审查。

### Owner 接收

- 按回报闭环等待和补读 NOTICE/RELAY_NOTICE；核对业务 task_id、当前有效版本、from 与 responds_to（须对应待回报清单中的原请求及预期收件人），不能仅凭交接版本推进。
- 不匹配的通知只作历史证据；旧格式缺关联字段时先按已核对来源与交接记录补证，无法关联则 BLOCKED。
- 只有 Worker 对执行/修复交接的 DONE 才触发实际 diff 与验收核对及后续审查；Advisor 答复只完成对应设计/审查请求，不重复发起同一审查。
- CONTEXT_READY 按压缩流程整理并恢复，原执行请求继续待回报；BLOCKED 先裁决问题；范围/方案/验收变化重新冻结新版。
- 没有人工审阅点时继续已授权的后续工作，不停在“已收到，等待用户让继续”。

### 执行巡检（Owner）

- Worker 或 Advisor 的单个请求持续 working 超过 30 分钟时，Owner 必须主动介入，不只被动等回报。
- 派出请求时用宿主支持的定时唤醒或后台计时设 30 分钟检查点，到点仍 working 则只读 get/read 该 pane 及其日志/产物，判断是否跑偏：固定长时间 sleep 空等、同一失败反复重试、读错或扩大范围、验证结果为 0 项却当通过、偏离冻结交接或在无关方向上消耗。
- 未跑偏则记录检查结果并再设下一个 30 分钟检查点；已跑偏则纠错：先用 herdr pane send-keys <pane> esc 中断当前空转或错误工具（只中断本编组角色，不碰用户 pane，不关闭 pane），待其 idle 后按发送前置发一条新 req 的纠偏消息，写明观察证据、应停止的做法和正确的下一步，不改变冻结范围；范围或方案需变更时按替代流程冻结新版。
- 纠错不代答审批、不替 Worker 写代码、不重置请求号。

### 独立 Advisor 输出

- 设计、局部答疑和最终审查遵守同一回报规则，按请求的 sync/async 回到 reply_to；无成立问题也要明确回报，不因没有修改建议而省略。
- 收到 NOTICE 仅作为状态通知时不回 ACK，避免通知互相触发。

## 任务目录与编组变更

### 任务目录

- 上方映射给出任务目录时，Owner 在首次冻结交接包时创建 <任务目录>/<task_id>/ 子目录，目录名只用字母、数字、点、下划线和连字符。
- 交接包、NOTICE、同步答复以及 Advisor 的设计/审查结论，完整正文各写成该子目录下的一个文件：Owner 的交接包以交接版本命名（如 op_x_h1.md），其余以 req 去掉 task_id 前缀后的部分命名（如 W1-003.md、A1-002.md）；各角色只写自己实例前缀的文件，先写同目录临时文件再改名，写后不再修改，修订使用新的请求号或交接版本。
- prompt 只带消息头、status、关联字段、一句摘要和文件路径，正文不再内联；写入文件不等于投递，仍须按上述规则实际投递并记录 delivery，收件人读到完整文件才算取得完整结果。
- 任务目录不是项目文件，不进入 diff、提交和审查范围：Advisor 可以写自己的结论文件，对项目文件仍保持只读；Worker 作为唯一项目代码 writer 的约束不变。
- 文件不含凭据、Cookie、Token 或可重放链接。
- 补读、RESULT_REPLAY、压缩后的恢复以及中途加入或替换的角色，先读该子目录中最新的有效文件，再向其他角色索取。
- 映射未给出任务目录时沿用正文内联。

### 任务目录清理

- 任务完成不等于删除授权，任何角色不自行删除任务目录或其中的文件。
- Owner 在任务交付时报告一次该任务子目录的路径；只有用户明确说可以删除后，才执行映射中的清理命令（把 <task_id> 换成实际目录名），不用 rm 直接删除，不加 --force，除非用户明确要求。
- 仍有 WAITING/PAUSED/BLOCKED 请求、READ_PENDING 或 RELAY_PENDING 的任务不清理，先说明卡点。

### 编组变更

- 用户可在任务中途增减独立 Advisor 或替换 Worker / Advisor，在册角色会收到 [TEAM_UPDATE]，以 team_rev 最大的一条为准更新名称→pane 映射、消息实例和分工；它是控制消息，不编号、不回 ACK，不改变 task_id、交接版本、冻结范围和授权。
- 被替换或移除的实例列在“已停用实例”中：不再向其派单或恢复，其晚到回报只作历史证据。
- 发给已停用实例且未结的请求，由 Owner 核对其已停止写入后按替代流程记 SUPERSEDED，并以新 req 向新实例发出自包含交接；新实例使用映射中的新实例号，序号从 000 开始，不继承旧实例的隐藏上下文。
- Advisor 被移除后，Owner 按无独立 Advisor 的规则自行承担审查。
