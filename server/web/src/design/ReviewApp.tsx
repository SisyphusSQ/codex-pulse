import {useEffect,useState} from 'react';
import {PulseApp,createQueryClient} from '../App';
import {api} from '../api/client';
import {installReviewApi} from './reviewApi';
import type {ReviewScenario} from './reviewFixtures';

export function ReviewApp({initialPath='/',scenario='expired'}:{initialPath?:string;scenario?:ReviewScenario}) {
 const [client]=useState(createQueryClient);
 const [ready,setReady]=useState(false);
 useEffect(()=>{
  const hash=location.hash;
  const restore=installReviewApi(scenario);
  location.hash=`#${initialPath}`;
  setReady(true);
  return ()=>{setReady(false);client.clear();api.setSession(null);restore();location.hash=hash;};
 },[client,initialPath,scenario]);
 return <div className="ds-live-review"><div className="ds-review-banner"><strong>TOO-523 · 当前实现预览</strong><span>全部统计与账号为合成样本 · 操作只在当前故事内存中生效</span><a href="/?path=/story/too-523-updates--index">查看本轮改动目录</a></div>{ready&&<PulseApp queryClient={client}/>}</div>;
}
