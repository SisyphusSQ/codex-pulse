import {afterEach,describe,expect,it,vi} from 'vitest';
import {cleanup,render,screen,waitFor} from '@testing-library/react';
import {App,ConfigProvider} from 'antd';
import {ReviewApp} from './ReviewApp';

vi.mock('../components/Chart',()=>({default:({label}:{label:string})=><div role="img" aria-label={label}/> }));
afterEach(cleanup);
describe('current production pages in Storybook',()=>{
 it.each([
  ['/','用量概览','年度用量汇总'],
  ['/usage/models','模型分析','用量模型'],
  ['/usage/sources','采集来源','来源与覆盖'],
  ['/quota','账号','Credits 余额与消耗'],
  ['/projects','项目','项目列表'],
  ['/sessions','会话','会话列表'],
  ['/pricing','价目表','价目平台'],
  ['/devices','设备与授权','刷新设备'],
 ])('renders %s through the current session contract',async(path,title,marker)=>{
  render(<ConfigProvider><App><ReviewApp initialPath={path}/></App></ConfigProvider>);
  await screen.findByRole('heading',{level:1,name:title});
  await waitFor(()=>expect(document.body.textContent).not.toContain('浏览器授权读取失败'));
  await waitFor(()=>expect(document.querySelector(`[aria-label="${marker}"]`)??screen.queryByText(marker)).not.toBeNull());
  expect(document.querySelector('.app-layout')).not.toBeNull();expect(document.querySelector('.ds-studio')).toBeNull();
 });
 it('renders the actual pairing screen',async()=>{
  render(<ConfigProvider><App><ReviewApp scenario="anonymous"/></App></ConfigProvider>);
  expect(await screen.findByRole('button',{name:'配对并进入'})).toBeInTheDocument();
 });
});
