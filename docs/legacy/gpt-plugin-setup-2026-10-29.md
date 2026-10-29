# RinoMesh · ChatGPT OAuth 接入（2026-10-29）

## 目标

ChatGPT **只填写一个 MCP 服务器 URL**：

`https://xuhuaai.top/mcp`

身份验证选择 **OAuth**。Gateway 是单用户单所有者架构，不引入多账号。OAuth 登录页使用现有的 Gateway 控制令牌证明所有者身份，随后 ChatGPT 获得独立的短期 OAuth Bearer 访问令牌。

## 当前实现

- `internal/oauthlocal`：MCP 受保护资源发现（RFC 9728）、授权服务器元数据（RFC 8414）、动态客户端注册（DCR）、授权码 + PKCE S256、刷新令牌及签名访问令牌校验。
- DCR 接受 ChatGPT 官方回调路径（`/connector/oauth/<callback_id>` 或 `/connector_platform_oauth_redirect`），注册结果保存到 Gateway 的状态目录，重启后仍然可用。
- `cmd/gateway`：只有 `/mcp` 为对外 MCP 服务。未认证请求返回 401 和 `WWW-Authenticate` 发现信息。已认证的 OAuth 访问令牌可调用 `list_devices`、`list_join_requests`、`approve_join_request`、`reject_join_request`、`execute_command`。已有控制 Bearer 令牌的兼容能力仍保留。
- Nginx 在公网暴露 OAuth 协议端点 `/.well-known/oauth-protected-resource/mcp`、`/.well-known/oauth-authorization-server`、`/register`、`/authorize`、`/token`。这些属于 **认证流程**，不是第二个 MCP 服务器。
- Nginx 将公网 `/mcp` 转发到 Gateway 同路径，并删除旧 `/mcp-admin` 路由。

## 腾讯云部署

已编译和测试的候选版本，以及配套 Nginx 配置与可回滚脚本，已经放在 `rino-tx`：

- `/home/ubuntu/rinomesh-gateway-oauth`
- `/home/ubuntu/rinomesh-mcp.conf.stage`
- `/home/ubuntu/activate-rinomesh-oauth.sh`

需要在腾讯云本地以有管理员权限的 Shell 执行一次：

```bash
sudo bash /home/ubuntu/activate-rinomesh-oauth.sh
```

该脚本保存现有 Gateway 可执行文件及 Nginx 配置备份，先验证 Nginx，再用原子重命名替换 Gateway 可执行文件，重启 Gateway 并等候健康检查，最后热重载 Nginx。如果失败则回滚，避免覆盖运行中的可执行文件引发 `Text file busy`。

当前 ChatGPT 远程工具无法执行 sudo；**在得到部署脚本成功输出之前，不能认为正式 OAuth 已上线。**

## ChatGPT 创建连接

1. 自定义 MCP 服务器 URL：`https://xuhuaai.top/mcp`。
2. 身份验证：**OAuth**。
3. 客户端注册方式如有选项，请选择 **Dynamic Client Registration (DCR)**；无需手工填写 Client ID / Client Secret。
4. 第一次登录会打开 `https://xuhuaai.top/authorize`，在这里输入腾讯云 `~/.config/rinomesh/control.token` 文件里的值，授权后自动返回 ChatGPT。令牌只在网页授权表单输入，别贴到聊天里。
5. 接入完成后在 ChatGPT 调用 `list_join_requests`、`approve_join_request`、`list_devices` 和 `execute_command` 验收。

ChatGPT 对 OAuth 的资源发现、DCR、PKCE、回调 URL 以及 `resource` 参数有明确规范；Gateway 访问令牌的 audience 固定为 `https://xuhuaai.top/mcp`。

## 检查

```bash
curl -i https://xuhuaai.top/mcp
curl https://xuhuaai.top/.well-known/oauth-protected-resource/mcp
curl https://xuhuaai.top/.well-known/oauth-authorization-server
```

第一条无凭据应返回 HTTP 401 及 `WWW-Authenticate`；后两条应返回 JSON。ChatGPT 发起登录后应显示授权页面。

测试实例已验证新 OAuth 实现 HTTP 401 发现头、PRM 和 AS 元数据、DCR 注册 HTTP 201；单元测试覆盖注册持久化、PKCE 验证、登录、访问令牌、刷新令牌、令牌错用及授权码重放。


## 11:02 OAuth 回调故障诊断

2026-10-29 11:01 首次提交控制令牌返回 HTTP 401；11:02:44 再次提交返回 HTTP 303，证明 Gateway 已验证令牌并发出 ChatGPT 回调重定向。但当时没有后续 `POST /token`，因此尚未完成 ChatGPT 侧的 token exchange。原客户端采用 `https://chatgpt.com/connector/oauth/{callback_id}` 回调。

已更新授权服务器：在 `/.well-known/oauth-authorization-server` 声明 `authorization_response_iss_parameter_supported: true`，并在成功授权回调中添加 `iss=https://xuhuaai.top`（RFC 9207）。新建客户端可使用 ChatGPT 的固定 `https://chatgpt.com/connector_platform_oauth_redirect` 回调。旧 DCR 客户端记录仍保留且可继续使用原回调 URI。

更新后需要重新编译部署：

```sh
cd /home/ubuntu/rinomesh-mvp-work
/opt/go/bin/go build -trimpath -o /home/ubuntu/rinomesh-gateway-oauth ./cmd/gateway
sudo bash /home/ubuntu/activate-rinomesh-oauth.sh
```

Go 测试 `go test ./internal/oauthlocal ./cmd/gateway -count=1` 已通过。部署成功后重新创建 ChatGPT 自定义 MCP 连接，以读取新增的 issuer-identification 能力，再进行授权。

若仍出现 303 后没有 `POST /token`，请保留 ChatGPT 回调页面显示的错误文本或浏览器 Network 中 callback 请求状态。OpenAI Developer Community 曾有 OAuth state 丢失、回调出错且不访问 token endpoint 的案例；这种情况需要区分客户端问题与 Gateway 问题，不能无根据地进一步改 Gateway。


## 授权成功但没有返回 ChatGPT（11:13 修复）

现象：第一次点击「授权连接」似乎无反应，第二次再次提交出现“Authorization request expired”。原因是旧实现首次通过校验后即删除待授权请求，并直接发送 HTTP 303。浏览器如果未完成跨站跳转，原页面无法恢复回调。

修复：首次校验成功后返回明确的「已授权 ChatGPT」页面，包含一秒自动跳转以及可手动点击的「返回 ChatGPT 完成连接」链接。同一个授权请求在五分钟内重新提交仍会返回相同回调地址，必须仍提供正确 Owner 控制凭据；不重复签发授权码。单元测试覆盖成功页、重试及错误控制凭据。

Go 实现位于 `internal/oauthlocal/oauthlocal.go`，测试位于 `internal/oauthlocal/oauthlocal_test.go`。

2026-10-29 本地验证：`go test ./internal/oauthlocal ./cmd/gateway -count=1` 通过，Gateway 构建候选文件为 `/home/ubuntu/rinomesh-gateway-oauth`。正式 Gateway 切换前不会生效。

部署仍使用：

```bash
sudo bash /home/ubuntu/activate-rinomesh-oauth.sh
```

部署后请回到 ChatGPT **重新发起一个新的 OAuth 连接授权**，不要继续提交旧浏览器标签页的表单。Gateway 服务重启会清除旧授权码/临时授权请求。新页面若没有自动返回，就点击页面上的「返回 ChatGPT 完成连接」。

若回调后 ChatGPT 显示错误，留意错误文字；不要分享包含 `code`、`state` 的完整跳转 URL。


## 11:25 授权成功但 MCP Action Discovery 返回 403

Nginx 访问日志显示 ChatGPT `POST /token` HTTP 200，紧接着 `openai-mcp/1.0.0` 对 `POST /mcp` 收到 HTTP 403（响应正文 45 字节）。已定位为 Go MCP SDK 对回环地址的 Host 校验：`Forbidden: invalid Host header "xuhuaai.top"`（含末尾换行，恰好 45 字节）。Nginx 原配置 `proxy_set_header Host $host` 使后端在 `127.0.0.1:18889` 看到外部 Host，触发 SDK 拒绝。OAuth 验证正常。

修复：公网 `/mcp` Nginx location 中使用 `proxy_set_header Host 127.0.0.1:18889;`，继续透传 OAuth `Authorization`。不关闭 SDK 的 localhost 校验，不修改 Gateway 或 OAuth 逻辑。新配置已写入腾讯云 `/home/ubuntu/rinomesh-mcp.conf.stage`，在云端执行一次：

```sh
sudo bash /home/ubuntu/enable-single-mcp.sh
```

脚本只重载 Nginx，不重启 Gateway，也不改已有设备连接。配置应用后，回到 ChatGPT 中刷新/重新发现 RinoCommander 工具。当前源码与 Nginx 修改已提交，**实际公网 Action Discovery 仍以用户重新连接后成功列出工具为验收标准**。
