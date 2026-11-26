import {useEffect,useState} from 'react';
import {PulseApp,createQueryClient} from '../App';
import {api} from '../api/client';
import {installReviewApi,type ReviewModelUsageInput} from './reviewApi';
import type {ReviewScenario} from './reviewFixtures';

export function ReviewApp({initialPath='/',scenario='current',ticket,previewTitle='当前 Web 实现',modelUsage}:{initialPath?:string;scenario?:ReviewScenario;ticket?:string;previewTitle?:string;modelUsage?:ReviewModelUsageInput}) {
 const [client]=useState(createQueryClient);
 const [ready,setReady]=useState(false);
 useEffect(()=>{
  const hash=location.hash;
  const restore=installReviewApi(scenario,modelUsage);
  location.hash=`#${initialPath}`;
  setReady(true);
  return ()=>{setReady(false);client.clear();api.setSession(null);restore();location.hash=hash;};
 },[client,initialPath,scenario,modelUsage]);
 return <div className="storybook-review"><div className="storybook-banner"><strong>{ticket?`${ticket} · `:''}{previewTitle}</strong><span>复用实际页面、主题与样式 · 合成数据 · 操作仅在当前故事内存生效</span></div>{ready&&<PulseApp key={`${initialPath}:${scenario}`} queryClient={client}/>}</div>;
}
