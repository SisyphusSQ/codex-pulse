# macOS 正式签名、公证与发行

本流程面向 Apple Silicon / macOS 15 的 App Store 外发行。产品版本、App、Helper、
tag 和发行资产绑定同一冻结 SHA，RPC 和数据库 schema 不随产品版本机械变化。
构建入口默认 `signed-notarized`，不会因签名或公证失败降级。

## 凭据与准备

- 在执行机器核对 `security find-identity -v -p codesigning`，使用有效的
  `Developer ID Application` SHA-1 指纹；私钥留在 Keychain，由 codesign 调用。
- 公证需要独立的 Apple 认证，签名证书不能替代。通过本机安全交互入口配置
  `xcrun notarytool store-credentials codex-pulse-notary`；也可先安全配置其他命名
  profile，再通过 `--notary-profile` 指定。不要把密码或私钥放入 argv、环境、日志
  或仓库。无人值守环境没有可用认证时停止，不能代用户生成或猜测密码。
- 构建前用 `notarytool history --keychain-profile` 确认认证；登录钥匙串需要人工
  解锁时报告阻碍，不运行自动 unlock，不降低 ACL 或钥匙串保护。
- 核对最新公开版本与 stable/prerelease 全部 appcast build；使用新的版本号和全局
  递增 build。仓库 clean main 快进同步后，完成版本/CHANGELOG 提交与推送，再冻结 SHA。
- Sparkle 继续沿用既有公钥、私钥和固定更新源；不能与 Apple 签名同时随意换钥。

## 构建与公证

```bash
bash scripts/macos/build-release-app.sh \
  --version 0.18.0 --build-number 76 \
  --sparkle-feed-url https://github.com/SisyphusSQ/codex-pulse/releases/download/updates/appcast.xml \
  --sparkle-public-key-file /secure/path/sparkle-public.key \
  --signing-identity "$DEVELOPER_ID_SHA1" \
  --notary-profile codex-pulse-notary --skip-app-tests
```

示例版本和 build 只代表一次候选，执行时重新确定。提交/发版收尾不重复测试；
开发阶段执行受影响的聚焦验证，发布后验收仅在用户明确要求时运行。

脚本按顺序执行：

1. 构建并组装同一 App，校对版本、架构、SDK、Sparkle、rpath 与本机路径泄漏。
2. 对全部 Mach-O 和嵌套 bundle 由内到外签名，保留 Sparkle 既有 entitlements，
   检查 Developer ID、Hardened Runtime、时间戳及完整 bundle 签名。
3. 提交临时 ZIP 公证；保存请求摘要和 submission ID，等待 Accepted 后向 App
   staple 票据，检查 Gatekeeper。
4. 从 stapled App 生成最终更新 ZIP，解压后再次核对签名、票据和 Gatekeeper。
5. 生成 DMG，检查挂载布局及内含 App；DMG 签名、公证、staple 并检查 acceptance。
6. 最终资产不再变化后生成 SHA256SUMS；Sparkle 只签该 exact ZIP。

产物、公证 JSON 和失败构建目录位于 `.artifacts/releases/<tag>/`，不得提交。
`--distribution unsigned` 仅在明确授权下用于 ad-hoc 分发，拒绝混入正式签名参数。

## 中断与恢复

`notary-app` 与 `notary-dmg` 的 `.sha256`、`.submit.json` 和 `.wait.json` 保存一次
提交的资产摘要、编号和结果。失败构建目录会保留，先检查原文件和 Apple 状态。

```bash
bash scripts/macos/notarize-release-archive.sh \
  /retained/build/notarization.zip codex-pulse-notary \
  /release/directory/notary-app
```

相同摘要与已知 submission ID 只继续等待，不重新提交。摘要不同、请求结果未知
或缺少编号时停止，必须核对原请求；不得删除记录来触发盲目重发。已生成的发行
文件也不会被下一次构建覆盖。Accepted 后的 App staple、最终 ZIP/DMG 组装可按
保留的原 App 继续；不要重签 App 后冒用旧的公证结果。

## 发布与验收

按项目发布 reference 创建 signed tag 和公开 Release；公开资产下载后比对摘要、
签名、公证、Gatekeeper，再最后发布固定 appcast。不得覆盖既有 tag 或版本资产。

用户要求验收时，使用保留下载隔离属性的公开 DMG 检查首启；真实 N-1 和实际
安装旧版分别通过 Sparkle 下载、验签、Helper clean shutdown、替换与重启。
真实 Home 启动必须显式绑定 Home 及 0700 私有 runtime，读回 Home 物理身份、
版本、Helper、偏好及数据状态；会读取 Session/JSONL，并可能写入 runtime、SQLite、
偏好及标准 housekeeping。正常下载确认不等于不受信任开发者拦截，不能靠清除
quarantine 或关闭 Gatekeeper 制造通过。

最终分别报告开发测试、构建、签名、公证、远端发布、首启及旧版升级的实际状态。
只有真实发布与验收完成后，才将成功流程同步到外层发版 Skill。

## 本次接入证据

目标版本为 `v0.18.0`，候选 build 为 `76`；当前仍未发布，变更保留在 Unreleased。

| 项目 | 实际结果 |
| --- | --- |
| 开发阶段聚焦测试 | 40 项通过：公证恢复/失败语义 6 项、发行 helper 25 项、Sparkle/appcast 9 项 |
| Shell 语法与项目 Skill 格式 | 通过；Skill 校验复用本机缓存的隔离 PyYAML 环境 |
| 无人值守签名 | 临时探针签名、时间戳、执行通过；保留的 `v0.17.2` App 副本整包重签名通过，覆盖全部嵌套 Mach-O |
| 正式发行构建 | 在长构建前因缺少公证 profile 停止；未生成 `v0.18.0` 发行资产 |
| Apple 公证与票据 | 未执行；本机未找到公证认证，`codex-pulse-notary` profile 查询返回缺失 |
| tag / Release / appcast | 未创建版本 tag 或 Release，公开更新源未修改 |
| 公开安装与真实升级 | 未执行，等待可公证的正式发行资产 |
| 外层发版 Skill | 保持原样，等待真实发布与验收成功 |

原始签名日志、构建前置检查日志及状态保存在本机 ignored release artifacts 中。
已签名旧版副本只是签名链路证据，不是新版本构建、公证、发布或产品验收证据。
