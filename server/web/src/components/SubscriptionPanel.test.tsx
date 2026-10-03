import {OperationNotifications} from './OperationNotifications';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {QueryClientProvider} from '@tanstack/react-query';
import {MemoryRouter} from 'react-router-dom';
import {createQueryClient} from '../App';
import {api} from '../api/client';
import {SubscriptionPanel} from './SubscriptionPanel';
import type {Subscription} from '../api/subscriptions';
const fetcher=vi.fn<typeof fetch>();
const value:Subscription={account_key:'synthetic',revision:'4',alias:null,automatic_plan:'plus',manual_plan:null,resolved_plan:'plus',date_kind:'monthly_renewal',renewal_day:31,membership_date:null,next_date:'2026-10-31',day_delta:29,date_state:'future',time_zone:'Asia/Shanghai',updated_at_ms:1};
beforeEach(()=>{api.setSession({client_id:'test',name:'测试',purpose:'admin',csrf:'synthetic-csrf',expires_at_ms:null});fetcher.mockReset();vi.stubGlobal('fetch',fetcher);});afterEach(()=>vi.unstubAllGlobals());
function mount(){render(<OperationNotifications><QueryClientProvider client={createQueryClient()}><MemoryRouter><SubscriptionPanel accountKey="synthetic" provider="codex" /></MemoryRouter></QueryClientProvider></OperationNotifications>);}
it('saves with revision and CSRF then reads authoritative result',async()=>{
 fetcher.mockImplementation(async (_,options)=>new Response(JSON.stringify({code:200,data:options?.method==='POST'?{...value,revision:'5',alias:'工作账号'}:value})));
 mount();await screen.findByText('每月31日续费');const user=userEvent.setup();await user.click(screen.getByRole('button',{name:'编辑订阅'}));await user.type(screen.getByLabelText('账号备注'),'工作账号');await user.click(screen.getByRole('button',{name:'保存订阅设置'}));await screen.findByText('订阅设置已保存到中心');expect(document.querySelector('.ant-notification')).toHaveTextContent('订阅设置已保存到中心');expect(document.querySelector('.subscription-panel .ant-alert-success')).toBeNull();const call=fetcher.mock.calls.find(([,o])=>o?.method==='POST')!;expect(JSON.parse(String(call[1]?.body))).toMatchObject({expected_revision:'4',alias:'工作账号',renewal_day:31,membership_date:null});expect(call[1]?.headers).toMatchObject({'X-Pulse-CSRF':'synthetic-csrf'});
});
it('preserves form after revision conflict',async()=>{
 fetcher.mockImplementation(async(_,options)=>options?.method==='POST'?new Response('',{status:409}):new Response(JSON.stringify({code:200,data:value})));
 mount();await screen.findByText('每月31日续费');const user=userEvent.setup();await user.click(screen.getByRole('button',{name:'编辑订阅'}));await user.click(screen.getByRole('button',{name:'保存订阅设置'}));await screen.findByText('订阅设置未保存');expect(screen.getByRole('button',{name:'保存订阅设置'})).toBeInTheDocument();expect(screen.getByText('数据发生冲突，请刷新后重新确认。')).toBeInTheDocument();
});
