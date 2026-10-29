# RinoMesh 单 Gateway 接入与验收

状态（2026-10-29）：自动化端到端测试通过；真实 rino-pc 的加入、GPT 审批及命令执行尚未完成。

## 验收标准

电脑安装一次新版 Windows Agent 后，只需在 PowerShell 运行：

    & "$env:LOCALAPPDATA\RinoMesh\rinomesh-agent-join.exe" join --device rino-pc-joined

Agent 将通过 wss://xuhuaai.top/agent/ws 发出接入申请，打印 request_id 并等待。GPT 调用 list_join_requests、approve_join_request，Agent 获得 approved 事件，保存本机 ticket 并自动上线；GPT 随后使用 execute_command(device_id=rino-pc-joined, command=hostname) 获得 Windows 的真实输出。

旧版 Agent 仍使用 rino-pc 作为设备 ID。测试时采用 rino-pc-joined 可避免两个进程抢占同一设备 ID。

## 实现

服务端 cmd/gateway/join.go：通过现有 /agent/ws 完成加入申请、审批事件、凭据下发；原有 MCP 私有控制端新增 list_join_requests / approve_join_request / reject_join_request，公有 /mcp 只增加只读 list_join_requests。批准记录由 RINOMESH_JOIN_DB 持久化。

客户端 cmd/agent/join.go：join 命令等待批准，保存凭据到 LOCALAPPDATA/RinoMesh/joined-DEVICE.json，随后常驻并自动重连。

Go 测试覆盖：加入、待审、GPT MCP 批准、获批准凭据、WebSocket 连接及 hostname 命令回传。旧版 Agent 的回归测试仍通过。

## 云端状态

当前正式 Gateway (127.0.0.1:18889) 未切换，依然正常运行。测试 Gateway (127.0.0.1:18890) 运行正常，仅供独立试验。

已备好以下文件：

- /home/ubuntu/rinomesh-gateway-join
- /home/ubuntu/rinomesh-agent-join.exe
- /home/ubuntu/activate-rinomesh-join.sh

正式启用需要在腾讯云执行一次：

    sudo bash /home/ubuntu/activate-rinomesh-join.sh

此脚本备份原 Gateway、安装 systemd StateDirectory=rinomesh 和持久数据库位置、重启服务并检查 /healthz，失败自动回滚。ChatGPT 的非特权远程终端尝试重启时收到 Interactive authentication required，因此这一步尚未执行。

正式部署后，Windows PowerShell 可以从云端复制编译产物：

    scp rino-tx:/home/ubuntu/rinomesh-agent-join.exe "$env:LOCALAPPDATA\RinoMesh\rinomesh-agent-join.exe"

随后执行上面的单行 join 即可。

## 当前阻碍

PC 的 Desktop Commander Remote 此刻已离线，因此未能完成真实客户端的程序交付。GPT 直接调用云端私有 MCP 审批接口时被 OpenAI 远程工具安全检查拦截；目前的公共 ChatGPT MCP 连接仍是只读，尚未具备审批与执行权限。不可声称真实验收已完成。

GitHub Actions 构建产物：https://github.com/RinoPaw/Rino-Network/actions/workflows/rinomesh-join.yml

## 本阶段不做

不加入账号注册、权限细分、Owner 私钥签名器及命令白名单。公网 /mcp 保持只读，避免匿名远程命令执行；私有 MCP 可以执行命令。


## 2026-10-29 12:01 接入恢复修复与实机状态

经过 GPT MCP 实机验收，旧 Agent `rino-pc` 已经执行 `hostname` 返回真实 Windows 主机名。新的 `rino-pc-joined` 申请 `join_4003e1d43678729f103960afeb97c72eff21086ea884151e` 已获 GPT 批准，但 Windows 等待进程在审批前退出、未持久化凭据，导致无法上线。

已开发并测试：Gateway 可以在已有设备获批后接受新的接入申请，只有新的申请再次获批才使旧接入凭据失效；Agent 从首次收到 `join_pending` 就保存含请求 ID 的接入凭据，断线后使用该凭据恢复同一申请，无需重复审批。测试模拟断线重连、Gateway 重启和设备重新入网，均通过。

新编译产物：`/home/ubuntu/rinomesh-gateway-join` 和 `/home/ubuntu/rinomesh-agent-join.exe`。Windows 的 `%LOCALAPPDATA%\\RinoMesh\\rinomesh-agent-join.exe` 已经更新，进程已启动。旧 Gateway 当前拒绝该设备的新申请并报告 `device already approved`；新版客户端会继续自动重试。

云端需要执行一次管理员部署：

```sh
sudo bash /home/ubuntu/activate-rinomesh-join.sh
```

此部署只替换 Gateway 二进制并验证 /healthz，不需要重改 Nginx 或 OAuth；原 `https://xuhuaai.top/mcp` 保持不变。部署后 Windows 的新版 Agent 会自动重新申请，GPT 使用 RinoCommander `list_join_requests` 找到新编号、调用 `approve_join_request`，然后对 `rino-pc-joined` 执行 `hostname` 取得真实结果。**在这条新版设备控制链路成功前，不宣称一条命令接入的实机验收完成。**
