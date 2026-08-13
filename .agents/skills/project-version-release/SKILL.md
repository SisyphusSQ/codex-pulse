---
name: project-version-release
description: 为 Codex Pulse 准备、检查和执行版本发布。用于版本规划、CHANGELOG 归档、macOS 发行资产检查、签名与公证门禁、Git signed tag、GitHub Release、Release Notes、首次安装说明、发布读回和发版证据汇总；也用于判断当前仓库为什么不能发布 stable 或 prerelease。
---

# Codex Pulse Version Release

把一次发布视为同一 App Bundle、同一 commit、同一 tag 和同一 GitHub
Release。Swift App 与 Go Helper 不得独立发版。

## 开始前

1. 读取仓库根级 `AGENTS.md`。
2. 读取
   [Codex Pulse release policy](references/codex-pulse-release-policy.md)。
3. 涉及 tag、GitHub Release 或资产上传时，再读取
   [GitHub release workflow](references/github-release.md)。
4. 发版前必须先同步远端主分支。在仓库根目录确认当前分支为 `main` 且工作树干净，
   然后执行：

```bash
git pull --ff-only origin main
```

不得使用 `--rebase`、`--autostash`、强制更新或自动 stash 来绕过该门禁。
如果工作树不干净、当前分支不是 `main`、`pull` 失败或无法 fast-forward，停止发版。
同步完成后重新读回 `HEAD`、`origin/main` 和工作树状态，要求工作树干净且
`git rev-parse HEAD` 等于 `git rev-parse origin/main`；只有此时才能冻结
`RELEASE_SHA` 并继续后续发版步骤。同步后若代码再次变化，必须重新执行本门禁。

5. 再运行只读检查：

```bash
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --version v0.1.0-beta.1 \
  --channel preview --json
```

如果工作树不干净、HEAD 未冻结或版本未显式给出，停止发版。stable 默认沿用
`unsigned stable`：必须按本仓库现行规则完成发行构建、验证、资产、tag、Release、
appcast 和公开读回，并在 Release Notes 如实披露 macOS 信任边界。只有显式选择
`signed-notarized`，且现场签名、公证、Gatekeeper 和最终资产 readback 全部通过时，
才允许按可信分发路径继续。保留并报告已有改动，不执行 reset、restore 或自动 stash。

分发模式与测试策略分开判断。`unsigned stable` 不代表跳过测试；默认仍执行
`make verify`、`make check`、`make test-go`、`make test-swift` 和真实 Codex Home
验收的现行规则。只有用户明确要求跳过某一项时，才能省略该项，并仍保留发行构建、
资产完整性、Sparkle、signed tag、Release、appcast 和公开读回。

## 选择动作

| 用户目标 | 动作 |
| --- | --- |
| 判断能否发版 | 运行 `check`，报告 blockers 与未观察 gate。 |
| 规划版本 | 运行 `version-plan`，区分 tag、Bundle version 与 build number。 |
| 归档 CHANGELOG | 先 dry-run `archive-changelog`，确认后才加 `--write`。 |
| 生成 Release Notes | 运行 `render-notes`；默认 stdout，加 `--write` 才写 `.artifacts/`。 |
| 准备 tag / Release | 运行 `release-plan`，再按 reference 分步执行并逐步读回。 |
| 发布 stable | 默认发布如实披露的 `unsigned stable`；`signed-notarized` 仅显式 opt-in，且必须通过完整签名、公证、Gatekeeper、首启、资产和 GitHub readback。 |
| 发布未签名预览版 | 必须显式授权、使用 prerelease tag，并采用 Gatekeeper 说明。 |

## 本地辅助命令

脚本默认不写文件。所有写入都要求 `--write`：

```bash
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  version-plan --version v0.1.0-beta.1 \
  --current-build-number 41 --build-number 42 --json

python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  archive-changelog --repo "$PWD" --version v0.1.0 \
  --date 2026-07-24 --json

python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  render-notes --repo "$PWD" --version v0.1.0-beta.1 \
  --channel preview --distribution unsigned \
  --summary "首个开发预览版" --json

python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  release-plan --repo "$PWD" --version v0.1.0-beta.1 \
  --channel preview --release-sha <40-character-commit-sha> --json
```

脚本不得执行 `git tag`、`git push`、`gh release`、签名、公证或上传。
`release-plan` 只在 release SHA 等于当前 clean `main` HEAD、最终 DMG、ZIP、
`SHA256SUMS` 和 Release Notes 四项资产齐全且摘要匹配、Release Notes 无占位符
且本地 tag 不存在时输出分阶段命令。

## 远端副作用边界

- 创建和推送 tag、直接创建公开 Release、上传资产分别先确认目标与授权。
- GitHub Release 创建成功后立即公开，不再经过 Draft 中间态；执行创建命令前必须确认 tag、Release Notes 和全部发行资产已完成最终检查。
- 使用 signed annotated tag；签名不可用时停止，不静默降级。
- 使用 `gh release create --verify-tag`，不允许 CLI 隐式创建 tag。
- 已推送 tag 不得强制移动。删除 tag、Release 或资产需要单独明确授权。
- 每次远端变更后读回 tag target、Release 状态、正文、资产与 SHA-256。

## Release Notes

以 [release notes template](assets/release-notes-template.md) 为模板。必须包含：

- 首次安装下载 DMG、Sparkle 使用 ZIP，并提醒不要下载 GitHub 的
  `Source code`；
- macOS 版本、CPU 架构和发布等级；
- 与实际签名状态匹配的首次打开方式；
- Codex Home、本地数据库、可选在线额度能力和隐私边界；
- 已知限制、SHA-256 和完整变更入口。

未签名发行版（包括默认的 `unsigned stable`）必须说明“不要移到废纸篓”，
再引导至“系统设置 → 隐私与安全性 → 仍要打开”。已签名、公证的 stable 版
不得复用该绕过说明；如果 `signed-notarized` 资产仍触发该提示，判定发布验证
失败。只有签名、公证和最终资产 gate 已读回时，才允许给 notes 添加
`--distribution signed-notarized`。stable 与 Preview 都必须按真实产物选择
`unsigned` 或 `signed-notarized`；`unsigned stable` 不得描述为 macOS 可信分发。

## 交付证据

分别报告 source/readback、local gate、isolated smoke、真实 Home 验收、
codesign、notarization、tag、GitHub Release 和资产校验。未实际执行的
CI、签名、公证、发布或最终用户首启不得描述为已完成。
