# RinoMesh · 多账号、设备申请与 Gateway 失陷隔离设计

状态：架构决策草案，2026-10-29。**未部署新的账号或端到端验签功能。** 此设计优先于继续对现有匿名公共 MCP 添加写操作。

## 目标和不做的事

- 一个 Gateway 承载多个账户，各账户有其设备集合；账户注册和登录可使用外部 OAuth/OIDC 身份提供方，账户键由 `(issuer, sub)` 唯一关联，另生成稳定内部 `account_id`。
- 从机主动选择 Gateway URL 和目标 `account_id`，提交设备接入申请；在 ChatGPT 登录对应账户后可查看申请并提出批准或拒绝。
- **攻击者即使拥有 Gateway 所在腾讯云服务器的 root 权限、数据库和该服务器上的所有密钥，也不能仅凭这些材料让诚实从机执行未经真正账户所有者授权的命令。**
- 第一版不区分目录读写、操作类型、按设备文件的细粒度权限。账户只有 Owner；配对成功的设备获得该账号的完整命令能力（仍以从机运行账号的系统权限为上限）。
- 暂不实现多级组织、角色 ACL、团队共享、复杂审核流程、集群和完整审计平台。

## 必须区分的两类认证

1. **ChatGPT → MCP/Gateway 的身份认证：** OAuth/OIDC Access Token，用于确认用户身份与账号归属、展示待批准设备、提交操作意图。令牌和账号记录对被入侵 Gateway 不构成从机的最终授权依据。
2. **账户所有者 → Agent 的操作授权：** 从机本地独立校验非对称签名。账户所有者私钥必须存储在 Gateway 之外，且不得作为 Gateway 的环境变量、数据库记录或中转令牌。Gateway 无权生成、替换或绕过从机的受信根公钥。

现有 `/mcp-control` 使用的本机 Bearer token，以及当前 `/agent/ws` 中的 Agent token，只能保护正常连接过程。当前 `cmd/agent/main.go` 会直接执行来自 WSS 的 `execute`，因此**尚不满足本设计的服务器失陷安全目标**。不能把现有 OAuth 验证当作目标已实现。

## 信任实体与长期密钥

- **Owner 身份密钥：** Ed25519 签名密钥对，私钥留在用户控制的独立签名器（例如手机 App/硬件安全存储或者非 Gateway 的独立服务），公钥在首次信任建立时直接绑定到从机本地。只在 Gateway 存储此公钥副本不能满足目标。
- **Agent 设备身份：** 每台从机首次安装时本机生成独立 Ed25519 密钥对，私钥仅保存在该从机本地，`device_id` 根据设备公钥计算，避免用可随意抢占的 `hostname` 作为唯一身份。显示名另存。
- **Gateway 身份：** 独立 TLS 证书与服务标识，只负责发现、连接、队列和转发；从机不能把 Gateway 的身份等同于命令授权者。
- **签名服务和会话委托：** GPT 需要持续自主执行时，签名操作必须由**独立于 Gateway 的可信签名端**完成。若需要用户每条命令批准，可由手机逐条显示完整命令并签名。若希望少交互，可由可信签名器核验独立认证的客户端请求后签署命令，或由用户签发短期、可撤销的会话委托给可信客户端；委托私钥仍不得放在 Gateway 上。仅将某个短期会话私钥交给 Gateway，会让攻击者在委托有效期内执行任意命令，无法满足严格威胁目标。

## 账号创建

1. OAuth/OIDC 认证账户，Gateway 建立内部 `account_id` 以及稳定的 `issuer,subject` 映射。
2. 用户在**独立签名器**中生成 Owner Ed25519 密钥，显示公钥指纹，Gateway 仅保存公钥与 key ID。
3. 账号注册、密钥轮换、撤销都需要 Owner 签名。身份提供方负责登录，不能未经 Owner 签名直接替换已配对从机的可信公钥。
4. Gateway 丢失数据库后，Owner 可以在新的 Gateway 建立账号映射；从机仍以先前绑定的 Owner 身份公钥判断签名。

## 从机申请接入

1. 从机指定 `gateway_url`、`account_id`，本机生成设备身份密钥及随机 `enrollment_nonce`，展示本机设备公钥指纹/二维码，并以 `pending` 状态向 Gateway 提交含设备公钥、名称、平台、到期时间的申请，附设备身份签名。
2. Gateway 在目标账户下创建唯一 `request_id`。该申请只是一条待审核记录，不赋予任何执行权限。
3. 用户登录 ChatGPT 的 MCP，调用 `list_join_requests`，核对来自从机**本地显示**的设备指纹和请求标识。GPT 可以调用 `approve_join_request(request_id)` 或 `reject_join_request` 表达决定。
4. **批准需要 Owner 私钥签名**完整内容：`gateway_id/account_id/device_pubkey/request_id/enrollment_nonce/expires_at`。签名必须通过独立可信签名器产生，签名器应展示并绑定这些字段；单纯 Gateway 返回“approved”、或 OAuth 登录通过都不能令从机变为 trusted。
5. 首次信任建立需要**从机侧本地可信引导**，将 Owner 公钥从用户签名器经现场二维码/直接导入和双端指纹核对绑定，或其他经独立验证的渠道传入。不能让 Agent 仅从被入侵的 Gateway 下载“Owner 公钥”并自动信任，否则攻击者会替换为自己的公钥。
6. 从机验证签名及本地受信 Owner 公钥，再将账号绑定写入本机；状态变为 `paired`，之后允许接收已签名的命令。若签名/指纹/nonce 不匹配，保持 `pending`。
7. 不同账号申请互相隔离：请求必须含 `account_id`，Gateway 查询按当前 OAuth `issuer,sub` 过滤；从机验签时也须比对已绑定账号 ID。

## 命令授权和回放防护

每条命令的签名覆盖一份确定性编码的信封，至少包括：
`protocol_version`、`gateway_id`、`account_id`、`device_id`、`task_id`、`command_bytes`（含 shell/执行类型）、`created_at`、`expires_at`、`nonce`、`authorization_key_id`，以及如有使用的 `session_id`。

- 签名前进行确定性序列化与域分离（例如 `RinoMesh/v1/execute\x00`）。无有效签名绝不调用 `os/exec`。公钥必须来自从机本地信任存储，不能由请求消息自行指定可信身份。
- 从机本地校验账号、设备、Gateway 身份绑定、签名、用途、过期时间及 nonce；持续保存已接受的 task IDs 或单调序列，防止 Gateway 在重连/重启后重放旧命令。
- 端到端签名仅保证授权和完整性；Gateway 仍可拦截、延迟、丢弃消息，也可能读取未加密命令和输出。要保护命令/输出内容的机密性，另加 Agent 与可信客户端之间的端到端加密。
- 所有 Agent 更新必须走有可信签名验证的发布流程；不能允许 Gateway 远程无条件替换 Agent 二进制，否则可以通过“更新”移除验签逻辑。
- 从机本机被入侵、Owner 签名器被入侵、用户错误批准钓鱼申请不在 Gateway-only compromise 的保证范围内。

## MCP 工具，第一版

- `register_account` / `get_account`：账号注册与状态，注册过程需要独立确认 Owner key。
- `list_devices`：仅显示登录账户自己的在线/离线设备。
- `list_join_requests`：查询登录账户的待审请求与设备指纹。
- `approve_join_request` / `reject_join_request`：表达决定并发起受信签名步骤；绝不能单凭 MCP 请求把设备变成 paired。
- `execute_command`：接受完整授权信封，Gateway 只转发；从机执行前完成签名与防重放验证。

不用为第一版设计 `read/write/admin` 角色或文件权限表。成功绑定账号即拥有该账号下设备的全量操作能力，命令本身仍需要 Owner 的可验证签名/有效授权。

## Gateway 最小数据模型

`accounts(account_id, issuer, subject, owner_key_id, owner_pubkey, created_at)`
`devices(device_id, account_id, device_pubkey, label, state, joined_at)`
`join_requests(request_id, account_id, device_pubkey, nonce, requested_at, expires_at, state)`
`authorized_signers(account_id, signer_key_id, signer_pubkey, state)`
`connections(device_id, online, last_seen)` 只需内存态。

需要持久保存账号/设备/待审数据，最初可以使用 SQLite；**数据库本身不是从机信任根**。数据库被篡改不应授予新设备或让 Agent 执行命令。

## 部署和验收顺序

**第一步：Agent 本地验签优先。** PC 目前离线，无法更新正在运行的 Windows Agent。可先在 Linux 测试 Agent / 模拟从机做红队验签测试；当前 PC Agent 保持“遗留、不满足服务器失陷保证”标记。

**第二步：多账号、注册和待审申请。** 复用现有 OAuth 接入代码，按 `issuer+sub` 映射账户，持久化账号/申请，保护所有账号相关 MCP 工具。

**第三步：可信 Owner 签名器和配对。** 先做人工确认的配对流程，再考虑 GPT 自动化短期会话授权；验收从机首次信任绑定与申请批准的真实签名。

**第四步：GPT 命令链路。** 用经过真实 Owner 签署的任务执行命令，确保 Gateway 仅能中转、不能制造授权。

**第五步：故意模拟 Gateway 失陷。** 测试攻击者拿到 root、Gateway Agent token、MCP token、SQLite、TLS 私钥后，无法直接下发 PowerShell；伪造账户映射、替换 owner_pubkey、重放签名命令、伪造接入批准、下发假 Agent 更新均被从机拒绝。

## 非目标 / 关键取舍

如果 GPT 被允许持续无人值守运行命令，签名器必须独立确认请求确实来自被授权 GPT/客户端。不能让被入侵 Gateway 任意要求签名器签任意命令；否则签名只是摆设。最省事的严格安全版本是**每条命令由用户的独立可信设备确认签名**；自主化需要另一个独立受信组件。这个约束源自对“腾讯云 root 被攻破仍无法控制从机”的要求，而非权限细分。

参考：MCP Authorization（OAuth 用于资源服务器认证），RFC 9421（端到端签名、签名覆盖字段和 replay 风险）。
