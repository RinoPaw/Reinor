# RFC-0001：RinoMesh 多账号、设备接入与不可信 Gateway

- 状态：**Draft v0.1**，供评审；未作为线上协议启用
- 日期：2026-10-29
- 适用范围：Go Gateway、Windows/Linux Agent、ChatGPT MCP 接入
- 原则：**Gateway 能传递请求，却不能自行批准设备或制造有效命令授权。**

## 1. 目标与边界

一个 Gateway 可以注册多个账号。每个从机选择目标 Gateway 和账号、主动提交加入申请；账号所有者通过登录后的 MCP 查看申请并批准。批准后的设备归属于该账号，账号所有者可以操作其设备。

**威胁假设：** 攻击者可能拿到腾讯云服务器的 root 权限、Gateway 程序/数据库、Gateway 的 TLS 和 MCP 密钥。只要从机自身和账号所有者的独立签名私钥仍安全，攻击者便不能让从机执行其伪造的命令，也不能仅靠篡改 Gateway 数据库给从机换主人。攻击者仍可断网、拒绝服务、伪造云端展示信息或删除云端数据。

本 RFC 不设计读/写/管理员权限层级、团队角色、文件白名单、命令白名单或细分操作授权。**账号所有者对已配对从机拥有完整命令能力，受从机操作系统运行权限约束。** 端到端内容加密暂不纳入本版；Gateway 仍可能看到命令与执行结果。任何客户端/从机软件更新机制也不能允许 Gateway 绕过这里的验签条件。

## 2. 三种身份以及一处不可回避的独立信任根

| 身份 | 产生/储存 | 作用 |
| --- | --- | --- |
| 账号登录身份 | OAuth/OIDC 提供方；Gateway 存储 `issuer + sub` 映射 | 确定 MCP 请求访问哪个账号、查询哪些申请 |
| 账号所有者身份（Owner Key） | **独立于 Gateway** 的可信签名端持有 Ed25519 私钥；从机本地保存公钥 | 批准设备加入、签署命令；它才是从机实际认可的授权者 |
| 设备身份（Device Key） | 从机安装时本地生成 Ed25519 密钥对、私钥不出设备 | 签署入网申请、确认设备公钥确实由该从机持有 |

`account_id = SHA256(owner_public_key)`，`device_id = SHA256(device_public_key)`（实际编码使用固定前缀与完整摘要）。`label` 只用来显示主机名，例如 `rino-pc`，不构成身份。Gateway 的 TLS 证书只用于运输连接，不能给执行命令的权限。

**首次绑定信任时必须有独立渠道**：用户在从机本地通过扫码、配置文件导入或直接核对指纹确认 Owner 公钥。Agent 不得从 Gateway 自动下载并无条件信任它声称的“账号 Owner 公钥”。

## 3. 账号注册

1. 用户在独立签名端生成 Owner Key，获得公钥与账号指纹。
2. 用户经 OAuth/OIDC 登录 Gateway；Gateway 根据 `(issuer, sub)` 创建账号管理记录，并保存账号公钥与 `account_id`。
3. 为避免其他已登录会话随意冒领同一公钥，首次注册向 Owner 签名端发送包含 Gateway 身份、账号 ID 和一次性随机数的注册挑战，由 Owner 私钥签署。
4. Gateway 验签后登记。**数据库登记只是展示和路由的依据**；对于从机，真正生效的是设备本地保存的 Owner 公钥。

同一个 Gateway 的不同账号不可互相枚举待接入设备、操作其他账号设备。账号更名或迁移 Gateway 不改变 Owner Key 的身份。Owner Key 的轮换需要另行定义由旧密钥或从机本地确认的过程，不能凭 Gateway 数据库改写完成。

## 4. 从机申请加入某个账号

从机由本地使用者配置 `gateway_url`、`account_id`，并独立确认对应 Owner 公钥指纹，然后提交：

```text
JoinRequest {
  version, gateway_id, account_id, request_id,
  device_id, device_public_key, label,
  nonce, expires_at, device_signature
}
```

`device_signature` 由设备私钥签署除签名字段外的规范化请求。Gateway 保存状态为 `pending`，向该账号的 MCP 提供 `list_join_requests`。

GPT 登录目标账号后可以调用 `approve_join_request` 表达批准意图。但**OAuth 登录成功不能代替 Owner 签名**：可信签名端需核对并签署完整的 `JoinApproval`，内容至少绑定 `gateway_id/account_id/request_id/device_id/device_public_key/nonce/expires_at`。Gateway 只负责转发。

从机把 Approval 与自己正在等待的 JoinRequest 逐字段匹配，并使用**本地预先信任的 Owner 公钥**验签。通过后在本地保存 `paired` 记录，并向 Gateway 返回签名确认；只有收到确认后，Gateway 才将该设备在账号下标记为已配对。拒绝或过期的请求不产生设备控制权。

**配对过程中用户需要完成一次独立指纹确认。** 不能只看 Gateway 页面上的设备名字就批准，否则恶意 Gateway 能虚构或调包请求；签名端也需要显示完整设备指纹。

## 5. 每次命令执行

```text
ChatGPT --OAuth--> Gateway MCP --请求待签名--> 可信签名端
ChatGPT/Gateway <--签名授权-- 可信签名端
Gateway --转发 SignedCommand--> Agent
Agent --本地验签、检查重放--> 执行 --> Gateway --> ChatGPT
```

签名覆盖确定性编码的整份命令信封（建议定义确定性 CBOR / 固定协议版本和域分离），至少包含：

```text
SignedCommand {
  version, gateway_id, account_id, device_id,
  task_id, shell, command_bytes, nonce,
  issued_at, expires_at, signer_key_id, signature
}
```

Agent 验证签名公钥与本地绑定的 Owner 或有效授权签名者一致，核对账号和目标设备，检查有效期，执行前持久化记录已接受的任务标识以阻止重放。没有签名、签名覆盖字段遭修改、账号不匹配或重放的消息，一律不进入系统命令执行入口。Gateway 即使被 root 控制也无法为不同命令重新生成有效签名。

### GPT 如何不必逐条找用户签字

第一版可以先使用**独立签名端人工批准**，确认协议可用后再做“短时自主会话”。若要 GPT 连续自主下发任意命令，可信签名端必须通过**不依赖 Gateway 自报身份**的方式验证请求来自用户授权的客户端/会话，且签名私钥不能存放在 Gateway 及其控制的容器、进程或服务器上。仅让 Gateway 在 OAuth 验证后自动请求签名端签名，遭入侵时仍会被滥用。

实现选项：ChatGPT 另连接一套独立部署的可信签名 MCP；或由用户控制的签名客户端签发短期会话。MCP 平台实际是否允许该工作流，需要之后做端到端验证。**本 RFC 暂不假设 GPT 已拥有可直接签名的私钥或原生设备密钥能力。**

## 6. Gateway 数据与 MCP 接口

最小持久数据：`accounts(account_id, issuer, sub, owner_pubkey)`、`join_requests(request_id, account_id, device_pubkey, nonce, expires_at, state)`、`devices(device_id, account_id, device_pubkey, label, state)`。可先使用 SQLite；它只保存路由和列表状态，不是 Agent 的信任根。

MCP 工具初稿：

- `get_account`、`register_account`：获取/创建登录账号以及注册挑战。
- `list_join_requests`：列出当前登录账号的待审批设备与完整指纹。
- `approve_join_request`、`reject_join_request`：记录决定、交付 Owner 签署的批准/拒绝。单纯调用批准工具不能让设备成为 trusted。
- `list_devices`：仅当前账号的设备。
- `execute_command`、`get_task_status`：提交签名任务、查询执行状态；Gateway 无签名权限。

从机到 Gateway 的 WSS 接口负责 `hello`、`join_request`、`join_approval`、`execute`、`result` 等信封。Yggdrasil 可继续作为网络承载或辅助发现，但它的连通状态不授予账号命令权限。

## 7. 迁移与验收

**现状：线上旧 Windows Agent 收到 `execute` 就会运行 PowerShell。它还不满足本 RFC 的 Gateway 失陷隔离要求。** Go `internal/trust` 中的设备身份、入网批准与命令验签仅完成库级实现与测试，尚未接入正式 Agent 的命令分发路径。

实现顺序：

1. 完成 Agent v2 的本地身份存储、待配对状态、签名验收与拒绝旧版裸 `execute`，在模拟从机测试。
2. 实现 Gateway 多账号与待批准列表，严格按当前 OAuth 账号筛选。
3. 实现可信签名端与首次配对；对真实从机执行签名测试。
4. 再接入 GPT 登录、申请审批、命令发起；逐步迁移 Windows Agent。**迁移完成前不可声称服务器被攻破也控制不了 Windows。**
5. 下线旧版无签名命令通道后，验收根权限攻击模拟：伪造批准、伪造命令、篡改字段、跨账号冒用、重放旧任务、修改数据库公钥、Agent 重启等均不能产生未授权执行。

## 8. 明确暂不实现

不做 ACL/RBAC、读写权限分级、命令白名单、自动软件升级、审计平台、多租户计费、复杂恢复密钥机构；不会借“安全”名义增加额外确认步骤。只有首次 Owner 公钥独立确认、设备加入的签名授权、每条命令的可信签名（可以由经过独立授权的签名器自动完成）属于实现目标所必需的信任机制。

## 9. 借鉴

- Tailscale **Tailnet Lock**：可信节点签发新节点密钥，其他节点独立验证；协调服务器无权自行引入新受信节点。
- Nostr **NIP-46**：独立的远程签名器和不保存签名私钥的客户端。
- **MCP Authorization / RFC 9728**：OAuth 资源服务器发现与客户端登录，只负责 ChatGPT 到服务端身份认证。

本草案是 RinoMesh 自己的协议草图，尚未形成兼容 Tailscale、Nostr 或 MCP 的独立签名协议。
