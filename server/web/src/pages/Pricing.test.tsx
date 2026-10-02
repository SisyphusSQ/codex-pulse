import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {render,screen} from '@testing-library/react';
import {MemoryRouter} from 'react-router-dom';
import {QueryClientProvider} from '@tanstack/react-query';
import {createQueryClient} from '../App';
import {api} from '../api/client';
import {referenceAmount,sourceLink} from '../api/catalog';
import Pricing from './Pricing';
beforeEach(()=>api.setSession({client_id:'test',name:'测试',purpose:'admin',csrf:'synthetic',expires_at_ms:null}));afterEach(()=>vi.unstubAllGlobals());
it('retains observed unpriced models and escapes model names',async()=>{
 const row={key:'one',provider:'codex',model:'<img src=x onerror=alert(1)>',mode:'已观测 · 参考价未知',currency:'USD',unit:'未知',input_price:null,cached_price:null,cache_write_price:null,output_price:null,version:'',source_url:'javascript:alert(1)',verified_at_ms:0,effective_from_ms:null,evidence:'observed',notes:'未定价'};
 vi.stubGlobal('fetch',async()=>new Response(JSON.stringify({code:200,data:{version:'test',models:[row],plans:[]}})));
 render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><Pricing /></MemoryRouter></QueryClientProvider>);await screen.findByText(row.model);expect(document.querySelector('img[src="x"]')).toBeNull();expect(screen.getAllByText('未公开')).toHaveLength(4);expect(screen.getByRole('link',{name:'查看用量'})).toHaveAttribute('href',expect.stringContaining('view=usage'));
});
it('formats rates without treating tiny positive amounts as free and restricts source links',()=>{expect(referenceAmount('0.002')).toBe('<$0.01');expect(referenceAmount('0.075')).toBe('$0.08');expect(referenceAmount(null)).toBe('未公开');expect(sourceLink('https://cursor.com/docs/models-and-pricing')).toBeTruthy();expect(sourceLink('https://cursor.com.evil.invalid')).toBeUndefined();expect(sourceLink('javascript:alert(1)')).toBeUndefined();});
