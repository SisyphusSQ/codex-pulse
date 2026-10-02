import { useEffect, useState } from 'react';
import { Alert, Button, Card, Descriptions, Form, Input, Modal, Select, Steps, Table, Tabs, Tag, Typography } from 'antd';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiError } from '../api/client';
import { getClients, issuePairing, renameClient, revokeClient, revokePairing, type Client, type Pairing } from '../api/access';
import { getDevices } from '../api/statistics';
import { useSession } from '../auth/SessionProvider';
import { EmptyState, ErrorState, LoadingState } from '../components/QueryState';
import { dateTime, integer, providerNames } from '../format';
import { duration } from '../components/QuotaViews';

const deviceStates:Record<string,string>={ready:'来源索引就绪',partial:'部分可用',disabled:'已关闭',reconnect_required:'需要重新配对',queue_full:'队列已满',source_unavailable:'来源不可用'};
const failMessage=(cause:unknown)=>cause instanceof ApiError?cause.message:'操作未完成，请稍后重试。';
export default function Devices(){
 const {session,logout,retry:reloadSession}=useSession();
 const queryClient=useQueryClient();
 const query=useQuery({queryKey:['clients'],queryFn:({signal})=>getClients(signal),refetchInterval:30_000});
 const devices=useQuery({queryKey:['devices','status'],queryFn:({signal})=>getDevices(signal),refetchInterval:30_000});
 const [form]=Form.useForm<{name:string;purpose:Client['purpose']}>();
 const [renameForm]=Form.useForm<{name:string}>();
 const [pairFormOpen,setPairFormOpen]=useState(false);
 const [pairing,setPairing]=useState<Pairing|null>(null);
 const [adminRequest,setAdminRequest]=useState<{name:string;purpose:Client['purpose']}|null>(null);
 const [revoking,setRevoking]=useState<Client|null>(null);
 const [renaming,setRenaming]=useState<Client|null>(null);
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState<string>();
 const [now,setNow]=useState(Date.now());
 useEffect(()=>{
  if(!pairing)return;
  const tick=()=>{const time=Date.now();setNow(time);if(pairing.expires_at_ms<=time)setPairing(p=>p?{...p,code:''}:null);};
  tick();const timer=setInterval(tick,1000);return()=>clearInterval(timer);
 },[pairing?.expires_at_ms]);
 async function refresh(){await Promise.all([query.refetch(),devices.refetch()]);}
 async function invalidate(){await Promise.all([queryClient.invalidateQueries({queryKey:['clients']}),queryClient.invalidateQueries({queryKey:['devices']}),queryClient.invalidateQueries({queryKey:['quotas']}),queryClient.invalidateQueries({queryKey:['statistics']})]);}
 async function issue(value:{name:string;purpose:Client['purpose']}){
  setBusy(true);setError(undefined);setPairing(null);
  try{const code=await issuePairing(value.purpose,value.name.trim());setNow(Date.now());setPairing(code);form.resetFields();setAdminRequest(null);setPairFormOpen(false);}catch(cause){setError(failMessage(cause));}finally{setBusy(false);}
 }
 function submit(value:{name:string;purpose:Client['purpose']}){if(value.purpose==='admin')setAdminRequest(value);else void issue(value);}
 async function cancelCode(){
  if(!pairing?.code)return;setBusy(true);setError(undefined);
  try{await revokePairing(pairing.code);setPairing(null);}catch(cause){setError(cause instanceof ApiError&&cause.status===409?'这张码已被消费，请在授权列表核对并撤销对应客户端。':failMessage(cause));}finally{setBusy(false);}
 }
 async function revoke(){
  if(!revoking)return;setBusy(true);setError(undefined);
  try{if(revoking.id===session?.client_id)await logout();else{await revokeClient(revoking.id);await invalidate();}setRevoking(null);}catch(cause){setError(failMessage(cause));}finally{setBusy(false);}
 }
 async function rename({name}:{name:string}){
  if(!renaming)return;setBusy(true);setError(undefined);
  try{await renameClient(renaming.id,name.trim());const self=renaming.id===session?.client_id;setRenaming(null);await invalidate();if(self)reloadSession();}catch(cause){setError(failMessage(cause));}finally{setBusy(false);}
 }
 return <section><div className="device-toolbar"><Button onClick={()=>void refresh()} loading={query.isFetching||devices.isFetching} aria-label="刷新设备">刷新</Button><Button type="primary" onClick={()=>setPairFormOpen(true)}>添加设备或浏览器</Button></div>
 {error&&<Alert type="error" showIcon className="form-alert" title={error} />}

 <Tabs items={[
 {key:'clients',label:'客户端授权',children:<> {query.isPending?<LoadingState />:query.error&&!query.data?<ErrorState error={query.error} retry={()=>void refresh()} />:query.data&&<>
 {query.error&&<Alert type="warning" showIcon title="刷新失败，保留上次授权列表" description={query.error.message} />}
 <div className="device-grid">{query.data.map(c=><Card key={c.id} title={c.name} className={c.id===session?.client_id?'device-card current-browser':'device-card'} extra={<Tag color={c.revoked_at_ms||c.expires_at_ms!=null&&c.expires_at_ms<=now?'default':'green'}>{c.revoked_at_ms?'已撤销':c.expires_at_ms!=null&&c.expires_at_ms<=now?'已过期':'有效'}</Tag>}><div className="metric-note">{c.purpose==='admin'?'管理浏览器':'采集 App'}{c.id===session?.client_id?' · 当前浏览器':''}</div><Descriptions size="small" column={1} items={[{key:'created',label:'创建',children:dateTime(c.created_at_ms)},{key:'expires',label:'到期',children:c.expires_at_ms==null?'至撤销前有效':dateTime(c.expires_at_ms)},{key:'received',label:'最后接收',children:dateTime(c.last_received_at_ms)},{key:'id',label:'原始 ID',children:<Typography.Text className="record-id" copyable>{c.id}</Typography.Text>}]} /><div className="device-actions"><Button aria-label="改名" disabled={busy} onClick={()=>{renameForm.setFieldsValue({name:c.name});setRenaming(c);}}>改名</Button><Button aria-label="撤销" danger disabled={busy||c.revoked_at_ms!=null} onClick={()=>setRevoking(c)}>撤销</Button></div></Card>)}</div>
 <div className="metric-note source-footnote">撤销停止后续接入，不删除历史事实。关闭本机同步保留队列；清理队列为单独操作。</div></>}</>},
 {key:'status',label:'上报状态',children:<>
 <Card title="采集设备上报状态" className="table-card">
 {devices.isPending?<LoadingState />:devices.error&&!devices.data?<ErrorState error={devices.error} retry={()=>void devices.refetch()} />:<>
 {devices.error&&<Alert type="warning" title="设备状态刷新失败，保留上次读取" description={devices.error.message} />}
 {(devices.data??[]).length?<Table rowKey={r=>`${r.id}:${r.provider}`} size="small" dataSource={(devices.data??[]).flatMap(d=>d.providers.length?d.providers.map(p=>({...p,id:d.id,name:d.name,revoked:d.revoked_at_ms,last_received:d.last_received_at_ms})):[{id:d.id,name:d.name,provider:'',version:'',collected_at_ms:null,coverage_start_ms:null,coverage_end_ms:null,pending_batches:0,status:'unknown',received_at_ms:0,stale:true,revoked:d.revoked_at_ms,last_received:d.last_received_at_ms}])} pagination={{pageSize:10,showSizeChanger:false}} scroll={{x:1260}} columns={[{title:'机器 / Provider',render:(_,r)=><>{r.name}<div className="metric-note">{providerNames[r.provider]??'尚无 Provider 状态'}{r.revoked?' · 已撤销':''}</div></>},{title:'版本',render:(_,r)=>r.version||'未上报'},{title:'原采集截至',render:(_,r)=>dateTime(r.collected_at_ms)},{title:'中心最后接收',render:(_,r)=>dateTime(r.last_received)},{title:'覆盖边界',render:(_,r)=><>{dateTime(r.coverage_start_ms)}<div>→ {dateTime(r.coverage_end_ms)}</div></>},{title:'待发送批次',render:(_,r)=>r.provider?integer(r.pending_batches):'未知'},{title:'来源状态',render:(_,r)=><>{deviceStates[r.status]??'未知'}<div className="metric-note">{r.stale?'采集证据陈旧':'有近期采集证据'}</div></>}]} />:<EmptyState description="尚无采集设备。请签发采集码，在 App 的多机中心设置中配对并启用。" />}
 <Typography.Paragraph type="secondary">来源索引就绪及接收成功不代表完整历史上传，也不证明机器在线；关闭 App 后停止采集与上报，下次启动增量补采。此处仅显示上次收到的有限状态。</Typography.Paragraph></>}
 </Card></>},
 ]} />
 <Modal open={pairFormOpen} title="签发一次性配对码" width={520} footer={null} onCancel={()=>!busy&&setPairFormOpen(false)}><Steps size="small" current={0} items={[{title:'选择用途'},{title:'取得配对码'},{title:'客户端接入'}]} /><Form name="issue-client" form={form} layout="vertical" initialValues={{purpose:'collector'}} onFinish={submit} disabled={busy} autoComplete="off">
 <div className="chart-grid"><Form.Item name="name" label="设备或浏览器名称" rules={[{required:true,whitespace:true,message:'请填写名称。'},{max:128,message:'名称最多 128 个字符。'}]}><Input maxLength={128} autoComplete="off" placeholder="例如：工作 Mac" /></Form.Item><Form.Item name="purpose" label="授权用途"><Select aria-label="配对码用途" options={[{value:'collector',label:'采集 App：仅上报与读取自身确认'},{value:'admin',label:'管理浏览器：查看中心数据与管理授权'}]} /></Form.Item></div>
 <Button type="primary" htmlType="submit" loading={busy}>签发配对码</Button><Typography.Paragraph type="secondary" className="sign-in-note">10 分钟有效、只可消费一次，用途固定。采集码不能登录管理页面；配对后 App 仍需要明确启用同步。HTTPS 与显式私网 HTTP 共用此流程。</Typography.Paragraph></Form></Modal>
 <Modal open={adminRequest!==null} title="签发管理浏览器码？" okText="确认签发管理码" cancelText="取消" confirmLoading={busy} onOk={()=>adminRequest&&void issue(adminRequest)} onCancel={()=>!busy&&setAdminRequest(null)}><Typography.Paragraph>使用这张码的浏览器可以读取账号、会话和所有统计，签发配对码及撤销客户端。名称：{adminRequest?.name}。</Typography.Paragraph></Modal>
 {pairing&&<Modal open title={pairing?.purpose==='admin'?'管理浏览器配对码':'采集 App 配对码'} footer={<><Button disabled={busy} onClick={()=>setPairing(null)}>仅关闭显示</Button><Button danger loading={busy} disabled={!pairing?.code} onClick={()=>void cancelCode()}>撤销未用码</Button></>} onCancel={()=>!busy&&setPairing(null)}><Typography.Paragraph>请将码输入对应客户端，勿发到公开渠道。只在当前页面内存显示。</Typography.Paragraph><div className="pairing-code">{pairing?.code||'配对码已到期'}</div><Typography.Paragraph type="secondary">到期：{dateTime(pairing?.expires_at_ms)} · 剩余 {duration(pairing?pairing.expires_at_ms-now:null)}。关闭显示不会撤销尚未使用的码；撤销未用码成功后立即失效。</Typography.Paragraph>{error&&<Alert type="error" title={error} />}</Modal>}
 <Modal open={revoking!==null} title="撤销客户端授权？" okText="确认撤销" cancelText="取消" confirmLoading={busy} onOk={()=>void revoke()} onCancel={()=>!busy&&setRevoking(null)}><Typography.Paragraph>撤销 {revoking?.name} 后，客户端无法继续接入，历史仍保留；恢复需要新码重新配对。{revoking?.id===session?.client_id?'这会退出当前管理浏览器。':''}</Typography.Paragraph></Modal>
 <Modal open={renaming!==null} title="修改客户端名称" footer={null} onCancel={()=>!busy&&setRenaming(null)}><Form name="rename-client" form={renameForm} layout="vertical" onFinish={rename} disabled={busy}><Form.Item name="name" label="名称" rules={[{required:true,whitespace:true,message:'请填写名称。'},{max:128,message:'最多 128 个字符。'}]}><Input maxLength={128} /></Form.Item><Button htmlType="submit" type="primary" loading={busy}>保存名称</Button></Form></Modal>
 </section>;
}
