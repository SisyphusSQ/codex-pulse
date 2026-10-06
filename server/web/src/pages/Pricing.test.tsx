import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {render,screen,within,waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {MemoryRouter} from 'react-router-dom';
import {QueryClientProvider} from '@tanstack/react-query';
import {createQueryClient} from '../App';
import {api} from '../api/client';
import {referenceAmount,sourceLink} from '../api/catalog';
import Pricing from './Pricing';
import {summaryFixture} from '../test/statisticsFixture';
import publicPlans from '../../../internal/service/catalog_srv/plans-20261002.json';
beforeEach(()=>api.setSession({client_id:'test',name:'测试',purpose:'admin',csrf:'synthetic',expires_at_ms:null}));afterEach(()=>vi.unstubAllGlobals());
it('retains observed unpriced models and escapes model names',async()=>{
 const row={key:'one',provider:'codex',model:'<img src=x onerror=alert(1)>',mode:'已观测 · 参考价未知',currency:'USD',unit:'未知',input_price:null,cached_price:null,cache_write_price:null,output_price:null,version:'',source_url:'javascript:alert(1)',verified_at_ms:0,effective_from_ms:null,evidence:'observed',notes:'未定价'};
 vi.stubGlobal('fetch',async()=>new Response(JSON.stringify({code:200,data:{version:'test',models:[row],plans:[]}})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><Pricing /></MemoryRouter></QueryClientProvider>);await screen.findByText(row.model);expect(document.querySelector('img[src="x"]')).toBeNull();expect(screen.getAllByText('未公开')).toHaveLength(3);expect(screen.getByRole('link',{name:'查看用量'})).toHaveAttribute('href',expect.stringContaining('/usage/models?provider=codex'));
});
it('formats rates without treating tiny positive amounts as free and restricts source links',()=>{expect(referenceAmount('0.002')).toBe('<$0.01');expect(referenceAmount('0.075')).toBe('$0.08');expect(referenceAmount(null)).toBe('未公开');expect(sourceLink('https://cursor.com/docs/models-and-pricing')).toBeTruthy();expect(sourceLink('https://cursor.com.evil.invalid')).toBeUndefined();expect(sourceLink('javascript:alert(1)')).toBeUndefined();});

it('keeps release order instead of promoting used old models and preserves filters and release evidence',async()=>{
 const base={provider:'codex',mode:'Standard',currency:'USD',unit:'1M tokens',input_price:'2',cached_price:null,cache_write_price:null,output_price:'10',version:'reference',source_url:'https://developers.openai.com/api/docs/pricing',verified_at_ms:Date.UTC(2026,9,2),effective_from_ms:Date.UTC(2026,9,2),evidence:'current',notes:''};
 const summary=summaryFixture(),usage={range:summary.range,scope:summary.scope,totals:summary.totals,coverage:summary.coverage,models:[{provider:'codex',model:'gpt-4.1',totals:summary.totals}],model_days:[],trend:[],providers:[],cursor_pools:[],model_cost_rounding_delta_micro_usd:'0',trend_cost_rounding_delta_micro_usd:'0'};
 const models=[{...base,key:'new',model:'gpt-6.1-sol',released_at_ms:Date.UTC(2026,8,29),release_source_url:'https://developers.openai.com/api/docs/changelog'},{...base,key:'old',model:'gpt-4.1',released_at_ms:Date.UTC(2025,3,14),release_source_url:'https://www.anthropic.com/claude-sonnet-5-5'},{...base,key:'unknown',model:'b-unknown',evidence:'observed',released_at_ms:null,release_source_url:''}];
 vi.stubGlobal('fetch',async(path:RequestInfo|URL)=>new Response(JSON.stringify({code:200,data:String(path).includes('/statistics/usage')?usage:{version:'test',models,plans:[]}})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><Pricing /></MemoryRouter></QueryClientProvider>);
 await screen.findByText('gpt-6.1-sol');await waitFor(()=>expect(screen.queryByText('已使用模型读取失败，目录仍可查，无法确认使用情况')).not.toBeInTheDocument());
 const rows=screen.getAllByRole('row');expect(rows.slice(1).map(row=>within(row).getByRole('link',{name:'查看用量'}).getAttribute('href'))).toEqual(models.map(r=>`/usage/models?provider=codex&model=${r.model}`));
 expect(screen.getByRole('columnheader',{name:'发布时间'})).toBeInTheDocument();expect(screen.getByText('2026-09-29')).toBeInTheDocument();expect(screen.getByText('2025-04-14')).toBeInTheDocument();
 await userEvent.setup().click(within(rows[1]).getByRole('button',{name:/展开行|Expand row/}));expect(await screen.findByRole('link',{name:'发布说明'})).toHaveAttribute('href','https://developers.openai.com/api/docs/changelog');expect(screen.getByText('核对日期')).toBeInTheDocument();
 await userEvent.setup().click(screen.getByText('近30天已使用'));await screen.findByText('gpt-4.1');expect(screen.queryByText('gpt-6.1-sol')).not.toBeInTheDocument();
});

it('groups modes around the standard rate, hides irrelevant API rows and keeps exact used-model links',async()=>{
 const base={provider:'codex',model:'gpt-6.1-sol',mode:'Standard · 短上下文',currency:'USD',unit:'1M tokens',input_price:'2',cached_price:'0.1',cache_write_price:'2.5',output_price:'10',version:'reference',source_url:'https://developers.openai.com/api/docs/pricing',verified_at_ms:Date.UTC(2026,9,2),effective_from_ms:null,evidence:'current',notes:''};
 const models=[{...base,key:'fast',mode:'Fast · 短上下文',input_price:'4'},{...base,key:'batch',mode:'Batch · 短上下文',input_price:'1'},{...base,key:'standard'},{...base,key:'long',mode:'Standard · 长上下文',input_price:'4'},{...base,key:'credits',model:'GPT-6.1 Sol',mode:'Standard · Codex Credits',currency:'credits',input_price:'2'},{...base,key:'embedding',model:'text-embedding-3-small',mode:'Embedding'},{...base,key:'old',model:'gpt-3.5-turbo'},{...base,key:'cursor-base',provider:'cursor',model:'Composer 2.5',mode:'文档价目'},{...base,key:'cursor-fast',provider:'cursor',model:'Composer 2.5 (Fast)',mode:'文档价目',input_price:'4'}];
 const summary=summaryFixture(),usage={range:summary.range,scope:summary.scope,totals:summary.totals,coverage:summary.coverage,models:[{provider:'cursor',model:'Composer 2.5 (Fast)',totals:summary.totals}],model_days:[],trend:[],providers:[],cursor_pools:[],model_cost_rounding_delta_micro_usd:'0',trend_cost_rounding_delta_micro_usd:'0'};
 vi.stubGlobal('fetch',async(path:RequestInfo|URL)=>new Response(JSON.stringify({code:200,data:String(path).includes('/statistics/usage')?usage:{version:'test',models,plans:[]}})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><Pricing /></MemoryRouter></QueryClientProvider>);
 await screen.findByText('gpt-6.1-sol');const row=screen.getByRole('row',{name:/gpt-6.1-sol Standard/});expect(within(row).getByText('$2.00')).toBeInTheDocument();expect(screen.getAllByRole('link',{name:'查看用量'})).toHaveLength(2);expect(screen.queryByText('text-embedding-3-small')).not.toBeInTheDocument();expect(screen.queryByText('gpt-3.5-turbo')).not.toBeInTheDocument();expect(screen.queryByText('Batch · 短上下文')).not.toBeInTheDocument();
 const user=userEvent.setup();await user.click(within(row).getByRole('button',{name:/展开行|Expand row/}));await user.click(screen.getByRole('button',{name:'其他计费条件（2）'}));expect(screen.getByText('Fast · 短上下文')).toBeInTheDocument();expect(screen.getByText('Standard · 长上下文')).toBeInTheDocument();expect(screen.queryByText('Batch · 短上下文')).not.toBeInTheDocument();await user.click(screen.getByRole('button',{name:'订阅 Credits 参考'}));expect(screen.getByText('Standard · Codex Credits')).toBeInTheDocument();
 await user.click(screen.getByRole('tab',{name:'订阅与额度'}));await user.click(screen.getByRole('tab',{name:'模型价格'}));await user.click(screen.getByText('近30天已使用'));await waitFor(()=>expect(screen.getAllByRole('link',{name:'查看用量'})).toHaveLength(1));expect(screen.getByRole('link',{name:'查看用量'})).toHaveAttribute('href','/usage/models?provider=cursor&model=Composer%202.5%20(Fast)');
});

it('preserves historical version deep links and reads old prices independently from the current reference',async()=>{
 const base={provider:'codex',model:'gpt-4.1',mode:'Standard · 本机历史基础文本',currency:'USD',unit:'1M tokens',input_price:'2',cached_price:'0.5',cache_write_price:null,output_price:'8',source_url:'https://developers.openai.com/api/docs/pricing',verified_at_ms:Date.UTC(2026,9,2),effective_from_ms:Date.UTC(2026,9,2),notes:''};
 const models=[{...base,key:'current',version:'reference',evidence:'current',input_price:'9'},{...base,key:'old',version:'old-version',evidence:'historical'},{...base,key:'new',version:'new-version',evidence:'historical',input_price:'3'}];
 vi.stubGlobal('fetch',async()=>new Response(JSON.stringify({code:200,data:{version:'test',models,plans:[]}})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={['/pricing?provider=codex&model=gpt-4.1&version=old-version']}><Pricing /></MemoryRouter></QueryClientProvider>);
 await screen.findByText('gpt-4.1');expect(screen.getByRole('radio',{name:'历史计价'})).toBeChecked();expect(screen.getByText('$2.00')).toBeInTheDocument();expect(screen.queryByText('$9.00')).not.toBeInTheDocument();await userEvent.setup().click(screen.getByText('相关模型'));expect(await screen.findByText('$9.00')).toBeInTheDocument();await userEvent.setup().click(screen.getByText('历史计价'));expect(await screen.findByText('$2.00')).toBeInTheDocument();
});

it('keeps an actually used non-text model with its own pricing unit',async()=>{
 const models=[{key:'image',provider:'grok',model:'grok-imagine-image',mode:'1K',currency:'USD',unit:'1 image',input_price:null,cached_price:null,cache_write_price:null,output_price:'0.02',version:'reference',source_url:'https://docs.x.ai',verified_at_ms:Date.UTC(2026,9,2),effective_from_ms:null,evidence:'current',notes:''}];
 const summary=summaryFixture(),usage={range:summary.range,scope:summary.scope,totals:summary.totals,coverage:summary.coverage,models:[{provider:'grok',model:'grok-imagine-image',totals:summary.totals}],model_days:[],trend:[],providers:[],cursor_pools:[],model_cost_rounding_delta_micro_usd:'0',trend_cost_rounding_delta_micro_usd:'0'};
 vi.stubGlobal('fetch',async(path:RequestInfo|URL)=>new Response(JSON.stringify({code:200,data:String(path).includes('/statistics/usage')?usage:{version:'test',models,plans:[]}})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><Pricing /></MemoryRouter></QueryClientProvider>);
 await screen.findByText('grok-imagine-image');expect(screen.getByText('USD / 1 image')).toBeInTheDocument();expect(screen.getByText('$0.02')).toBeInTheDocument();expect(screen.queryByText(/USD \/ 百万 Token/)).not.toBeInTheDocument();
});

it('groups real subscription tiers and keeps annual, regional and unknown price evidence',async()=>{
 vi.stubGlobal('fetch',async()=>new Response(JSON.stringify({code:200,data:{version:'test',models:[],plans:publicPlans}})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><Pricing initialTab="plans"/></MemoryRouter></QueryClientProvider>);
 await screen.findByRole('heading',{name:/^Pro$/});expect(screen.getAllByRole('heading',{level:3})).toHaveLength(4);expect(screen.queryByText('示例账号在用')).not.toBeInTheDocument();
 const user=userEvent.setup();await user.click(screen.getByText('团队与企业'));expect(await screen.findByRole('heading',{name:'Business'})).toBeInTheDocument();expect(screen.getByText('$25.00')).toBeInTheDocument();
 await user.click(screen.getByRole('combobox',{name:'Business套餐档位'}));await user.click(screen.getByText('Business · $20.00 · 每席位/月（年付）',{exact:true}));expect(await screen.findByText('$20.00')).toBeInTheDocument();expect(screen.getAllByText(/年付/).length).toBeGreaterThan(0);
 await user.click(screen.getByRole('tab',{name:/^Cursor$/}));await user.click(screen.getByText('地区与其他渠道'));expect(await screen.findByText('₹649.00')).toBeInTheDocument();expect(screen.getAllByText('仅印度，官网含税价').length).toBeGreaterThan(0);
 await user.click(screen.getByRole('tab',{name:/^Grok$/}));expect(await screen.findByRole('heading',{name:'SuperGrok Plus'})).toBeInTheDocument();await user.click(screen.getByText('个人'));expect(await screen.findByRole('heading',{name:'SuperGrok Lite'})).toBeInTheDocument();expect(screen.getAllByText('未公开')).toHaveLength(2);expect(screen.queryByRole('heading',{name:'SuperGrok Plus'})).not.toBeInTheDocument();
 await user.click(screen.getByRole('tab',{name:'模型价格'}));expect(screen.getByRole('radio',{name:'相关模型'})).toBeChecked();
});

it('shows DSH OpenAI reference and historical prices with DSH usage links',async()=>{
 const base={provider:'dsh',model:'gpt-6.1-sol',mode:'OpenAI Standard · API 公价估算',currency:'USD',unit:'1M tokens',input_price:'2',cached_price:'0.1',cache_write_price:null,output_price:'10',version:'openai-api-2026-09-29',source_url:'https://developers.openai.com/api/docs/pricing',verified_at_ms:Date.UTC(2026,8,30),effective_from_ms:Date.UTC(2026,8,29),notes:'不是 Codex 订阅实际扣费或额度'};
 vi.stubGlobal('fetch',async()=>new Response(JSON.stringify({code:200,data:{version:'test',models:[{...base,key:'reference',evidence:'current'},{...base,key:'history',evidence:'historical'}],plans:[]}})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter initialEntries={['/pricing?provider=dsh']}><Pricing /></MemoryRouter></QueryClientProvider>);
 await screen.findByText('gpt-6.1-sol');expect(screen.getByText('$2.00')).toBeInTheDocument();expect(screen.getByRole('link',{name:'查看用量'})).toHaveAttribute('href','/usage/models?provider=dsh&model=gpt-6.1-sol');
 await userEvent.setup().click(screen.getByText('历史计价'));expect(await screen.findByText('gpt-6.1-sol')).toBeInTheDocument();expect(screen.getByText('$0.10')).toBeInTheDocument();
});
