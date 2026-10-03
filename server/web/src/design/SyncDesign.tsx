import {useState} from 'react';
import {Alert,Button,Card,Descriptions,Modal,Segmented,Space,Tag,Typography} from 'antd';
import {OperationNotifications,useOperationNotifications} from '../components/OperationNotifications';
import favicon from '../assets/favicon.svg';

export function NotificationDesign() {
 return <OperationNotifications><NotificationActions/></OperationNotifications>;
}
function NotificationActions(){
 const notify=useOperationNotifications();
 return <div className="ds-component-preview"><Card title="操作通知"><p>一次操作结果显示在右上角；窄屏显示在顶部。数据质量与未完成的任务状态仍在相关区域保留。</p><Space wrap><Button type="primary" onClick={()=>notify.success('订阅设置已保存到中心')}>预览保存成功</Button><Button onClick={()=>notify.error('操作未完成','数据发生冲突，请刷新后重新确认。',()=>notify.success('已恢复可编辑的设置'))}>预览失败与重试</Button></Space></Card></div>;
}
export function FullSyncDesign(){
 const [phase,setPhase]=useState('idle'),[confirm,setConfirm]=useState(false),[paused,setPaused]=useState(false);
 const running=phase==='sending'||phase==='blocked';
 const state=phase==='completed'?'已完成':running?paused?'已暂停，启用后续传':phase==='blocked'?'来源不可用，保留进度':'进行中':'等待增量同步';
 return <div className="ds-component-preview"><Card title="客户端中心同步 · 交互样稿" className="ds-sync-preview" extra={<Tag>原生 App 中操作</Tag>}>
  <Descriptions size="small" column={1} items={[{key:'scope',label:'当前上报范围',children:'已启用 Provider 与当前 Home；示例为 Codex、Cursor'},{key:'interval',label:'增量上报间隔',children:'600 秒'},{key:'state',label:'同步状态',children:state}]}/>
  <div className="ds-sync-actions"><Button onClick={()=>setPhase('idle')} disabled={running}>增量补传</Button><Button type="primary" onClick={()=>setConfirm(true)} disabled={running}>全量补传…</Button>{running&&<Button onClick={()=>setPaused(!paused)}>{paused?'恢复同步':'暂停同步'}</Button>}</div>
  {phase!=='idle'&&<div className="ds-sync-progress"><Descriptions size="small" column={1} items={[{key:'task',label:'全量补传',children:state},{key:'snapshots',label:'已排入完整快照',children:phase==='completed'?'745':'120'},{key:'batches',label:'已确认上传批次',children:phase==='completed'?'37':'8'},{key:'started',label:'任务开始',children:'2026-10-03 09:00'}]}/>{phase==='blocked'&&<Alert type="warning" title="Cursor 来源尚不可用" description="已有队列与任务进度保留，不能把当前任务标为完成。"/>}</div>}
  <Typography.Paragraph type="secondary">退出 App 后停止采集和上报；下次启动续传。全量补传重新导出当前范围的完整快照，保留未确认队列和历史事实。</Typography.Paragraph>
  <div className="ds-sync-actions"><Segmented aria-label="模拟全量状态" value={phase} onChange={value=>{setPhase(String(value));setPaused(false);}} options={[{label:'待开始',value:'idle'},{label:'进行中',value:'sending'},{label:'来源受阻',value:'blocked'},{label:'完成',value:'completed'}]}/></div>
  <Modal title="全量补传当前范围？" open={confirm} onCancel={()=>setConfirm(false)} okText="开始全量补传" cancelText="取消" onOk={()=>{setPhase('sending');setConfirm(false);}}><p>重新导出全部已启用 Provider 的完整快照。已有未确认批次保留，中心按稳定 ID 和单调修订接收，不重复累计用量。</p><p>此故事只模拟交互，不调用 Helper，也不发起实际补传。</p></Modal>
 </Card></div>;
}
export function MetricsDesign(){
 return <div className="ds-component-preview"><Card title="Prometheus 指标接入 · 说明"><p>独立只读指标凭证访问 <Typography.Text code>/metrics</Typography.Text>。账号、邮箱、会话正文与凭据不进入指标标签。</p><Descriptions column={{xs:1,md:2}} items={[{key:'auth',label:'认证',children:'独立 Bearer 指标凭证'},{key:'permissions',label:'权限',children:'仅指标读取，不能访问业务 API'},{key:'source',label:'同步证据',children:'原采集时间与中心接收时间分别暴露'},{key:'pool',label:'运行指标',children:'HTTP、Go runtime、数据库连接池'}]}/><pre className="ds-metrics-sample">{`# 合成示例，不是实时抓取结果\npulse_metrics_database_up 1\npulse_source_pending_batches{client_id="demo-device",provider="codex"} 8\npulse_source_sync_healthy{client_id="demo-device",provider="codex"} 1\npulse_source_collected_timestamp_seconds{client_id="demo-device",provider="codex"} 1790902800\npulse_source_received_timestamp_seconds{client_id="demo-device",provider="codex"} 1790989200`}</pre><p>此页用于说明后端能力，没有新增正式 Web 运维页面或模拟 Prometheus 成功接入。</p></Card></div>;
}
export function FaviconDesign(){
 return <div className="ds-component-preview"><Card title="浏览器 SVG 图标"><Space size={24}><img src={favicon} width={64} height={64} alt="Codex Pulse 标签页图标"/><div><strong>Codex Pulse</strong><p>SVG 随 Web 资源构建；匿名静态资源可读取。</p></div></Space></Card></div>;
}
