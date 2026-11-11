import {afterEach,describe,expect,it,vi} from 'vitest';
import {ApiClient,ApiError} from '../api/client';
import {installReviewApi} from './reviewApi';

let restore:(()=>void)|undefined;
afterEach(()=>{restore?.();restore=undefined;vi.unstubAllGlobals();});
describe('Storybook synthetic API boundary',()=>{
 it('supports current session renewal and prevents unknown APIs reaching a real server',async()=>{
  const network=vi.fn<typeof fetch>();vi.stubGlobal('fetch',network);
  restore=installReviewApi('current');const client=new ApiClient();
  client.setSession(await client.session());expect((await client.renewSession()).purpose).toBe('admin');
  await expect(client.get('/api/v1/not-a-preview-route')).rejects.toMatchObject({status:501});
  expect(network).not.toHaveBeenCalled();
 });
 it('supports pairing and logout only inside story memory',async()=>{
  restore=installReviewApi('anonymous');const client=new ApiClient();
  await expect(client.session()).rejects.toBeInstanceOf(ApiError);
  client.setSession(await client.pair('SYNTHETIC-STORY-CODE'));expect((await client.session()).client_id).toBe('story-browser');
  await client.logout();await expect(client.session()).rejects.toMatchObject({status:401});
 });
 it('keeps empty annual statistics and record pages empty',async()=>{
  restore=installReviewApi('empty');const client=new ApiClient();
  const annual=await client.get<{heatmap_activity:{total_tokens:string|null};heatmap_totals:{cost_micro_usd:string|null}}>('/api/v1/statistics/annual');
  expect(annual.heatmap_activity.total_tokens).toBeNull();expect(annual.heatmap_totals.cost_micro_usd).toBeNull();
  for(const path of ['/api/v1/projects','/api/v1/sessions'])expect((await client.get<{items:unknown[]}>(path)).items).toEqual([]);
 });
 it('filters paid Credits with the selected account',async()=>{
  restore=installReviewApi('current');const client=new ApiClient();
  const value=await client.get<{paid_credits:unknown[]}>('/api/v1/quotas',{account_key:'account-two'});
  expect(value.paid_credits).toEqual([]);
 });
});
