# Codex Pulse 中心运行监控

[大盘 JSON](codex-pulse.json) 面向日常巡检、发布观察与故障定位，参考 Family System 运行监控。使用已有 `prometheus-local` 数据源与 `codex-pulse` job；默认最近1小时、30秒刷新，提供环境、Server实例、上报机器与Provider筛选。指标已由 Server `/metrics` 提供，安装大盘不需要升级客户端或重建 Server。

## 安装与维护

先备份现有 Prometheus 配置，将[采集任务模板](prometheus.yml)追加到 `scrape_configs`，保留其他任务与规则。目标地址使用实际 Server 监听地址；专用凭据使用 `server.metricsTokenFile` 所指的既有0600文件，不能复用浏览器或设备凭证，不将Token写入配置、大盘或仓库。使用已有 Prometheus 的 SIGHUP 热加载；不重启或删除TSDB。

适用机器：SQMC04。将 JSON 安装到 `/opt/homebrew/var/lib/grafana/dashboards/codex-pulse.json`，现有 Local file provider 每30秒自动加载；UID为 `codex-pulse-runtime`，无需重启 Grafana。本机访问 `/d/codex-pulse-runtime`。源配置在本仓库维护，Grafana临时编辑可能被文件同步覆盖。

现有指标以认证客户端ID关联来源，不携带机器名称。采集任务先将 `client_id` 复制成 `collector`；本机私有配置可按已确认的客户端ID，通过精确的 `metric_relabel_configs` 映射为SQMC03/04/05。未匹配的来源保留ID，不猜测机器名。更换授权或改名后同步私有映射；实际ID不写入模板。

## 面板与口径

- 顶部：进程CPU占主机逻辑核容量、实际RSS内存、采集状态、数据库查询状态、本次进程HTTP 5xx累计数及机器待传快照。
- HTTP：请求速率、4xx/5xx比例、P50/P95/P99、路由Top10；排除 `/metrics`、`/health`、`/ready`。上传单独展示状态码速率、P95及失败比例，请求数包含幂等重试，不是新增事实或Token。
- 来源：最后接收与原采集距今、同步状态、各机器待传。来源同步值1仅表示最近15分钟内同步检查ready，0可为陈旧、未知或失败，不等同于数据完整覆盖。
- 数据库/Go：连接池、等待次数速率、堆内存、Goroutine/线程、运行时长和GC平均暂停。没有连接池上限指标，不能计算利用率；Goroutine不代表in-flight请求。
- 明细：来源采集距今及机器待传两个表格，均按主要值倒序。

Helper将同一全局待传总数重复上报到多个Provider，因此按 `collector,instance` 取最大值，再按机器比较或求和；不能直接跨Provider相加。待传是最后上报快照，Provider筛选不改变机器队列面板。机器名是采集来源，不推断会话执行设备。

CPU分母沿用SQMC04的本机 `node_exporter`（`127.0.0.1:9100`）逻辑核数，迁移时同步容量来源。RSS与Go堆分开。HTTP 5xx顶部数值是进程生命周期累计值，不随所选范围变化；没有对应状态序列时保留无数据。大盘不展示虚构的应用版本：现有Server未暴露产品build_info；Go版本也不等同于产品版本。

所有无数据保持未知，不用零代替采集失败；无请求/无GC时比例与分位可为NaN。来源采集时间在未来时保留负距今，便于发现时间异常。指标历史从接入后开始，不补造历史；业务Token、费用与账号额度仍以中心Web为准。

## 读回与回退

读取 Prometheus `/api/v1/targets`、`up{job="codex-pulse"}`、配置热加载状态和真实指标，使用 Grafana最终查询与默认/代表性非默认筛选检查结果，再看实际大盘。采集up、数据库up与业务5xx分别展示，不能用ready200宣称全部业务接口正常。

回退时恢复私有备份中的 Prometheus 配置并SIGHUP，再撤回本次新增JSON；保留其他任务、TSDB、现有Grafana大盘、数据库及Server授权。Dashboard配置不包含Token、私有地址、实际客户端ID或原始会话内容。
