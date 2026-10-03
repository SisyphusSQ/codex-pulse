# 新增业务模块

1. 阅读根 AGENTS.md、[业务子包约定](../../architecture/packages.md)、[模型约定](../../architecture/models.md) 和本目录 code-style.md；确定业务域及各层子包，在 `details/<domain>/` 写清范围、接口、身份与资源授权、失败语义和验证入口。
2. 有持久化时先确定初始化表结构，在 `models/do/<store>/<domain>_do/` 建立 DO，再在 `repository/<store>/<domain>_repo/` 实现数据访问。MySQL DO 一个表一个文件，DO 与 TableName() 同文件；无存储业务可跳过 DO/repository，不造占位实现。
3. 在 `models/vo/<domain>_vo/` 定义 HTTP 请求/响应，在 `models/dto/<domain>_dto/` 定义需要跨服务或外部系统传输的对象。单文件业务也必须分包；已有类型先复用同一语义，不跨边界混用。
4. 在 `service/<domain>_srv/` 实现业务规则、依赖调用、模型转换和必要同库事务。保留 context、超时和错误链，不向 service 根包添加业务实现。
5. 在 `controller/<domain>_controller/` 实现解析、校验、身份提取、调用和响应；各层根 module.go 通过 Fx 注册子包的 Provide/Invoke。子包不得反向依赖装配包。
6. 配置新外部组件时同步 enabled、校验、OnStart/OnStop、就绪和关闭行为，构造阶段不得访问网络。
7. 为实际行为补充测试，检查业务根包没有新增平铺文件、MySQL DO 一表一文件及 import/Fx 装配是否完整，更新设计索引、配置和初始化文档和验证记录。跨平台相关改动执行 make build-all；正式平台运行另行记录。

目录说明始终保留；没有 examples 时也不把规范移入会被过滤的 example_* 目录。

SQL 目录、unreleased 清单与版本归档遵循 [SQL 规范](../../../sqls/README.md)，不在示例目录放建表或种子 SQL。
