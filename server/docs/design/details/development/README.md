# 开发与验证

| 命令 | 范围 |
|---|---|
| make build | 本机二进制，bin/ |
| make build-all / release-all | 六平台构建，见 [构建约定](../build/README.md) |
| make release | trimpath 构建；不执行测试、不发布 |
| make test | Go 单元测试和 race |
| make verify | 格式检查、test、vet、全包构建 |
| make lint | 固定版本 golangci-lint，只检查 |
| make vuln | 固定版本 govulncheck |
| make fmt | gofmt 改写源码 |
| make integration | 显式 integration 标签测试，需要隔离服务 |

外部服务测试和普通单测隔离。行为改动覆盖成功、失败、取消和退出路径。生成代码存在、构建成功、单元测试通过、容器运行和真实业务验收是不同证据。

请求参数在 controller 通过 BindAndValidate 统一校验；service 保留业务规则。返回错误保留 errors.Is/As 可识别语义，交给 HTTP 边界映射稳定状态；500 不回显内部消息。

提交与发布收尾复用开发阶段已有证据，遵守用户不重复测试的约定。缺少工具或服务时准确记录 Not Run/Blocked。

新增代码必读 [模型规范](../../architecture/models.md)、[代码规范](code-style.md) 和 [模块开发步骤](add-module.md)。
