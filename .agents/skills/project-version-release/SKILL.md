---
name: project-version-release
description: 为 Codex Pulse 准备、检查和执行版本发布。用于版本规划、CHANGELOG 归档、macOS 发行资产检查、签名与公证门禁、Git signed tag、GitHub Draft Release、Release Notes、首次安装说明、发布读回和发版证据汇总；也用于判断当前仓库为什么不能发布 stable 或 prerelease。
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
4. 先运行只读检查：

```bash
python3 .agents/skills/project-version-release/scripts/project_version_release.py \
  check --repo "$PWD" --version v0.1.0-beta.1 \
  --channel preview --json
```

如果工作树不干净、HEAD 未冻结、版本未显式给出或 stable gate 未通过，停止
发版。保留并报告已有改动，不执行 reset、restore 或自动 stash。

## 选择动作

| 用户目标 | 动作 |
| --- | --- |
| 判断能否发版 | 运行 `check`，报告 blockers 与未观察 gate。 |
| 规划版本 | 运行 `version-plan`，区分 tag、Bundle version 与 build number。 |
| 归档 CHANGELOG | 先 dry-run `archive-changelog`，确认后才加 `--write`。 |
| 生成 Release Notes | 运行 `render-notes`；默认 stdout，加 `--write` 才写 `.artifacts/`。 |
| 准备 tag / Release | 运行 `release-plan`，再按 reference 分步执行并逐步读回。 |
| 发布 stable | 要求完整签名、公证、首启、资产和 GitHub readback 证据。 |
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
`release-plan` 只在 release SHA 等于当前 clean `main` HEAD、最终三项资产和
SHA-256 匹配、Release Notes 无占位符且本地 tag 不存在时输出分阶段命令。

## 远端副作用边界

- 创建和推送 tag、创建 Draft Release、上传资产分别先确认目标与授权。
- 发布 Draft 前再次取得明确授权。
- 使用 signed annotated tag；签名不可用时停止，不静默降级。
- 使用 `gh release create --verify-tag --draft`，不允许 CLI 隐式创建 tag。
- 已推送 tag 不得强制移动。删除 tag、Release 或资产需要单独明确授权。
- 每次远端变更后读回 tag target、Release 状态、正文、资产与 SHA-256。

## Release Notes

以 [release notes template](assets/release-notes-template.md) 为模板。必须包含：

- 下载哪个资产，并提醒不要下载 GitHub 的 `Source code`；
- macOS 版本、CPU 架构和发布等级；
- 与实际签名状态匹配的首次打开方式；
- Codex Home、本地数据库、可选在线额度能力和隐私边界；
- 已知限制、SHA-256 和完整变更入口。

未签名预览版必须说明“不要移到废纸篓”，再引导至
“系统设置 → 隐私与安全性 → 仍要打开”。已签名、公证的 stable 版不得复用
该绕过说明；如果 stable 资产仍触发该提示，判定发布验证失败。
只有签名、公证和最终资产 gate 已读回时，才允许给 stable notes 添加
`--distribution signed-notarized`。Preview 也必须按真实产物选择
`unsigned` 或 `signed-notarized`。

## 交付证据

分别报告 source/readback、local gate、isolated smoke、真实 Home 验收、
codesign、notarization、tag、GitHub Release 和资产校验。未实际执行的
CI、签名、公证、发布或最终用户首启不得描述为已完成。
