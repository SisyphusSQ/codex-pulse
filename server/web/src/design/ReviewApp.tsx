import {useEffect,useState} from 'react';
import {PulseApp,createQueryClient} from '../App';
import {api} from '../api/client';
import {installReviewApi} from './reviewApi';
import type {ReviewScenario} from './reviewFixtures';

export function ReviewApp({initialPath='/',scenario='current',ticket}:{initialPath?:string;scenario?:ReviewScenario;ticket?:string}) {
 const [client]=useState(createQueryClient);
 const [ready,setReady]=useState(false);
 useEffect(()=>{
  const hash=location.hash;
  const restore=installReviewApi(scenario);
  location.hash=`#${initialPath}`;
  setReady(true);
  return ()=>{setReady(false);client.clear();api.setSession(null);restore();location.hash=hash;};
 },[client,initialPath,scenario]);
 return <div className="storybook-review"><div className="storybook-banner"><strong>{ticket?`${ticket} · `:''}当前 Web 实现</strong><span>复用实际页面、主题与样式 · 合成数据 · 操作仅在当前故事内存生效</span></div>{ready&&<PulseApp key={`${initialPath}:${scenario}`} queryClient={client}/>}</div>;
}
