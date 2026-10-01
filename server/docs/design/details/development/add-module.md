# 新增业务模块

1. 阅读根 AGENTS.md、architecture/models.md 和本目录 code-style.md；在 details/<domain>/ 写清范围、接口、身份与资源授权、失败语义和验证入口。
2. 有持久化时先确定初始化表结构与 DO，再实现 repository；无存储业务可跳过 DO/repository，不造占位实现。
3. 在 models/vo 定义 HTTP 请求/响应，在 models/dto 定义需要跨服务或外部系统传输的对象。已有类型先复用同一语义，不跨边界混用。
4. 实现 service 的业务规则、依赖调用、模型转换和必要同库事务。保留 context、超时和错误链。
5. 实现 controller，只做解析、校验、身份提取、调用和响应；在各层 module.go 通过 Fx 注册 Provide/Invoke。
6. 配置新外部组件时同步 enabled、校验、OnStart/OnStop、就绪和关闭行为，构造阶段不得访问网络。
7. 为实际行为补充测试，更新设计索引、配置和初始化文档和验证记录。跨平台相关改动执行 make build-all；正式平台运行另行记录。

目录说明始终保留；没有 examples 时也不把规范移入会被过滤的 example_* 目录。

SQL 目录、unreleased 清单与版本归档遵循 [SQL 规范](../../../sqls/README.md)，不在示例目录放建表或种子 SQL。
