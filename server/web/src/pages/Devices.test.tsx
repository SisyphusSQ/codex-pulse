import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClientProvider } from '@tanstack/react-query';
import { createQueryClient } from '../App';
import { SessionProvider } from '../auth/SessionProvider';
import Devices from './Devices';

const fetcher=vi.fn<typeof fetch>(),success=(data:unknown)=>new Response(JSON.stringify({code:200,data}));
const browser={client_id:'browser-one',name:'合成管理浏览器',purpose:'admin',csrf:'synthetic-csrf',expires_at_ms:Date.now()+86400000};
const client={id:'device-one',purpose:'collector',name:'<img src=x onerror=alert(1)>',created_at_ms:Date.now(),expires_at_ms:null,revoked_at_ms:null,last_received_at_ms:null};
beforeEach(()=>{fetcher.mockReset();vi.stubGlobal('fetch',fetcher);});afterEach(()=>vi.unstubAllGlobals());
function mount(){render(<QueryClientProvider client={createQueryClient()}><SessionProvider><Devices /></SessionProvider></QueryClientProvider>);}
function base(path:RequestInfo|URL){const p=String(path);if(p.endsWith('/session'))return success(browser);if(p.endsWith('/clients'))return success({clients:[client]});if(p.endsWith('/devices/status'))return success([{id:client.id,name:client.name,revoked_at_ms:null,last_received_at_ms:null,providers:[]}]);return success({applied:true});}
describe('device management',()=>{
 it('gives rename its own labeled field and writes only the name through the authenticated endpoint',async()=>{
  let renamed=false;
  fetcher.mockImplementation(async(path)=>{if(String(path).endsWith('/device-one/rename')){renamed=true;return success({applied:true});}if(String(path).endsWith('/clients'))return success({clients:[{...client,name:renamed?'新的合成名称':client.name}]});return base(path);});
  mount();await screen.findByText('客户端授权');const user=userEvent.setup();await user.click(screen.getByRole('button',{name:'改名'}));const dialog=await screen.findByRole('dialog');const input=within(dialog).getByRole('textbox',{name:'名称'});await user.clear(input);await user.type(input,'新的合成名称');await user.click(within(dialog).getByRole('button',{name:'保存名称'}));
  await screen.findByText('新的合成名称');const call=fetcher.mock.calls.find(([p])=>String(p).endsWith('/device-one/rename'))!;expect(JSON.parse(String(call[1]?.body))).toEqual({name:'新的合成名称'});expect((call[1]?.headers as Record<string,string>)['X-Pulse-CSRF']).toBe('synthetic-csrf');
 });
 it('keeps pairing code only in transient UI, protects admin issue with confirmation, and revokes unused code using CSRF',async()=>{
  fetcher.mockImplementation(async(path,options)=>String(path).endsWith('/pairings')?success({code:'SYNTHETIC-PAIR-CODE',purpose:JSON.parse(String(options?.body)).purpose,expires_at_ms:Date.now()+600000}):base(path));mount();
  const user=userEvent.setup();await screen.findByText('客户端授权');await user.type(screen.getByRole('textbox',{name:'设备或浏览器名称'}),'第二管理浏览器');await user.click(screen.getByRole('combobox',{name:'配对码用途'}));await user.click(await screen.findByText('管理浏览器：查看中心数据与管理授权',{selector:'.ant-select-item-option-content'}));await user.click(screen.getByRole('button',{name:'签发配对码'}));
  await screen.findByText('签发管理浏览器码？');expect(fetcher.mock.calls.some(([p])=>String(p).endsWith('/pairings'))).toBe(false);
  await user.click(screen.getByRole('button',{name:'确认签发管理码'}));await screen.findByText('SYNTHETIC-PAIR-CODE');expect(window.localStorage.length).toBe(0);expect(window.sessionStorage.length).toBe(0);
  await user.click(screen.getByRole('button',{name:'撤销未用码'}));await waitFor(()=>expect(screen.queryByText('SYNTHETIC-PAIR-CODE')).not.toBeInTheDocument());
  const call=fetcher.mock.calls.find(([p])=>String(p).endsWith('/pairings/revoke'))!;expect(JSON.parse(String(call[1]?.body))).toEqual({code:'SYNTHETIC-PAIR-CODE'});expect((call[1]?.headers as Record<string,string>)['X-Pulse-CSRF']).toBe('synthetic-csrf');
 });
 it('escapes names, treats never-reported provider as unknown, and keeps failed revocation visible without deleting history',async()=>{
  fetcher.mockImplementation(async(path)=>String(path).endsWith('/device-one/revoke')?new Response('',{status:503}):base(path));mount();await screen.findByText('客户端授权');expect(document.querySelector('img[src="x"]')).toBeNull();expect(await screen.findByText('尚无 Provider 状态')).toBeInTheDocument();
  const user=userEvent.setup();await user.click(screen.getByRole('button',{name:/^撤销$/}));const modal=await screen.findByRole('dialog');expect(within(modal).getByText(/历史仍保留/)).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([p])=>String(p).endsWith('/revoke'))).toBe(false);await user.click(within(modal).getByRole('button',{name:'确认撤销'}));await screen.findByText('中心暂时不可用，请检查服务状态后重试。');expect(screen.getAllByText(client.name).length).toBeGreaterThan(0);expect(screen.getByRole('dialog')).toBeInTheDocument();
 });
});
