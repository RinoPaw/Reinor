# Android ARM64 CLI Agent (Termux-first)

The existing Reinor Gateway accepts Android agent identities and uses the same
outbound WSS and owner-approved `join` protocol as Windows/Linux. **No Android
APK, screenshot backend, Android foreground service, or root escalation is
implemented in this stage.**

## Build

Build on Windows with Go 1.25+ and Android NDK 28 (adjust NDK path and API
level to the host environment). Android **must** use a system libc-backed DNS
resolver, so do not use `CGO_ENABLED=0` when cross-compiling the production
CLI. Go's pure-Go resolver reads `/etc/resolv.conf`, which Android normally
does not provide.

PowerShell:

```powershell
$ndk = "D:\Environment\Android\sdk\ndk\28.2.13676358\toolchains\llvm\prebuilt\windows-x86_64\bin"
$env:GOOS = "android"
$env:GOARCH = "arm64"
$env:CGO_ENABLED = "1"
$env:CC = Join-Path $ndk "aarch64-linux-android24-clang.cmd"
go build -trimpath -o reinor-agent-android-arm64 ./cmd/agent
```

For native on-device Termux builds, install `golang` and `clang` and build
with Termux's native Android libc toolchain. Test DNS resolution from that
specific build before enrolling the device.

## Running on the intended phone

From **Termux**, install the trusted ARM64 binary in its private home
directory (do not launch it from shared storage, which can be mounted noexec):

```sh
mkdir -p "$HOME/.local/bin"
# Download a binary from the owner-verified Reinor build, then verify SHA-256.
chmod 700 "$HOME/.local/bin/reinor-agent-android-arm64"
"$HOME/.local/bin/reinor-agent-android-arm64" join \
  -gateway https://xuhuaai.top -device rino-marble
```

The Agent requests enrollment via `wss://xuhuaai.top/agent/ws` and prints a
`join_...` request ID. **An operator must approve the precise device ID.**
After approval, credentials are saved as
`$HOME/.config/RinoMesh/joined-rino-marble.json` and used for reconnects.

Android executes commands with `/system/bin/sh -c`, in the **Termux user
process context**. The Termux environment and PATH are inherited from the
Termux session. Root access through KernelSU is deliberately not requested
or implied by joining. Do not run the Agent as root unless a separate owner
decision expressly authorizes it. A persistent session grants command
execution under the Agent user, so keep the enrollment file private and avoid
sharing the executable with credentials embedded (it embeds none).

## Known limits

- Android screenshot capture advertises `CAPABILITY_UNSUPPORTED`.
- Termux/Android may stop long-running background work because of battery,
  phantom-process, or vendor-specific lifecycle restrictions. Continuous
  operation, Termux:Boot, and foreground service behavior are not guaranteed
  until tested on the target handset.
- Android and Linux users may have different shell commands and file paths;
  the Gateway does not silently translate Linux commands to Android.
- A pure Go `CGO_ENABLED=0` Android build can start but generally cannot
  resolve DNS without explicit system integration; don't silently hardcode
  a public DNS server or disable TLS certificate validation.
- If a PC ADB session reports a different model than the intended handset,
  do not enroll it using the intended handset's ID.

## Minimum acceptance tests

1. Verify device model and Android API level before deploying to it.
2. Launch the Agent **without root** and confirm WSS enrollment.
3. Approve the request through `list_join_requests` and `approve_join_request`.
4. Verify `list_devices` reports `os=android`, then execute only `id`,
   `uname -a`, `pwd`, and a basic DNS/HTTPS diagnostic.
5. Disconnect/reconnect Wi-Fi and cellular connectivity and verify automatic
   reconnection and preserved credentials.
6. Check that the Agent stops when the user terminates its process and that
   Android capture reports unsupported.

Do not configure automatic startup until these checks pass on the actual
phone.
