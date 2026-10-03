import {useEffect,useState,type ReactNode} from 'react';
import {QueryClientProvider} from '@tanstack/react-query';
import {MemoryRouter,Route,Routes,useLocation} from 'react-router-dom';
import {createQueryClient} from '../App';
import {api} from '../api/client';
import {OperationNotifications} from '../components/OperationNotifications';
import Pricing from '../pages/Pricing';
import Usage from '../pages/Usage';
import {Studio} from './Studio';
import {installReviewApi} from './reviewApi';

function PricingSurface({subscriptionContent,initialTab}:{subscriptionContent:ReactNode;initialTab:'models'|'plans'}){
 const location=useLocation();
 const models=location.pathname==='/usage/models';
 return <Studio page={models?'models':'pricing'} hideToolbar reviewContent={<Routes><Route path="/pricing" element={<Pricing subscriptionContent={subscriptionContent} initialTab={initialTab}/>}/><Route path="/usage/models" element={<Usage/>}/></Routes>}/>;
}
// 同一价目表保留真实模型价格组件，仅在订阅标签内替换待评审样稿。
export function PricingReview({subscriptionContent,initialTab='plans'}:{subscriptionContent:ReactNode;initialTab?:'models'|'plans'}){
 const [client]=useState(createQueryClient);
 const [ready,setReady]=useState(false);
 useEffect(()=>{
  const restore=installReviewApi('expired');
  setReady(true);
  return ()=>{client.clear();api.setSession(null);restore();};
 },[client]);
 return ready?<OperationNotifications><QueryClientProvider client={client}><MemoryRouter initialEntries={['/pricing']}><PricingSurface subscriptionContent={subscriptionContent} initialTab={initialTab}/></MemoryRouter></QueryClientProvider></OperationNotifications>:null;
}
