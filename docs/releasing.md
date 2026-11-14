# GitHub Releases and branch policy

Reinor uses **main** as the only long-lived development branch. The archived Windows 7 preview remains available through historic Git tags and GitHub Releases; those binaries are not supported by the current Go 1.25-based Windows 10/11 build.

## Release contract

- Source of truth: GitHub repository `RinoPaw/Reinor`, branch `main`.
- CI: every push and pull request runs Go tests, vet, and cross-compilation.
- Releases: a maintainer **explicitly pushes a version tag** `vMAJOR.MINOR.PATCH` pointing to a commit already in `main`. The release workflow refuses a tag whose commit is not contained in `main`.
- GitHub Actions compiles Agent binaries for Windows amd64, Linux amd64 and Linux arm64, plus the Linux amd64 Gateway. It packages the Win11 one-time support launcher and generates `SHA256SUMS`.
- The release uploads these binaries and a checksum manifest to the public GitHub Releases page. No files are copied from a developer workstation. No production Gateway, Agent, client, registry, token or credential is updated by publishing a release.
- Downloads should use **versioned** release assets, never build artifacts from an arbitrary local machine. The checksum file detects corruption but cannot substitute for publisher identity or a signature; code signing / provenance attestation are future improvements.

## One-time Windows support

The recipient downloads `reinor-agent-windows-amd64.zip` from the selected GitHub Release and extracts it. They double-click `Start-Temporary-Support.cmd`, verify what remote command access means, and give the operator the `join_...` Request ID. The randomly generated `temp-support-...` device ID avoids reusing another recipient's active session. The operator approves the corresponding request and revokes it afterward. Running it does not require local administrator privileges.

Permanent enrollment continues to use `reinor-agent.exe join -device DEVICE_ID` and writes a persistent credential with the user's explicit authorization. Temporary enrollment uses `-temporary` and stores no credential.

## Version creation

After validating the exact main revision and CI results:

```sh
git checkout main
git pull --ff-only origin main
git tag -a v0.1.0 -m "Reinor v0.1.0"
git push origin v0.1.0
```

The tag in this example is illustrative; **do not run it blindly**. Each release version should be chosen after review. Check `https://github.com/RinoPaw/Reinor/releases` and confirm assets/checksums once CI completes.

## Branch retirement

Before deleting an old branch, compare its tip with main and either merge valuable unique commits, preserve an intentionally separate compatibility snapshot under a Git tag, or explicitly record why its work is obsolete. Deleting a ref does not uninstall any previously deployed Agent or Gateway. Never delete the branch currently selected as GitHub's default.
