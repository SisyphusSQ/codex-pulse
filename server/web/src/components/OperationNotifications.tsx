import {createContext, useContext, type ReactNode} from 'react';
import {Button, Grid, notification} from 'antd';

type Feedback = {success(title:string):void;error(title:string,description:string,retry?:()=>void):void};
const Context=createContext<Feedback|null>(null);

// 仅响应用户操作；查询刷新与持续的数据质量状态不进入通知队列。
export function OperationNotifications({children}:{children:ReactNode}){
 const screens=Grid.useBreakpoint();
 const [api,holder]=notification.useNotification({placement:screens.md?'topRight':'top',maxCount:3});
 const feedback:Feedback={
  success:title=>api.success({title,duration:4,role:'status'}),
  error:(title,description,retry)=>api.error({title,description,duration:0,role:'alert',actions:retry?<Button size="small" onClick={retry}>重试</Button>:undefined}),
 };
 return <Context.Provider value={feedback}>{holder}{children}</Context.Provider>;
}
export function useOperationNotifications(){
 const value=useContext(Context);
 if(!value)throw new Error('OperationNotifications provider required');
 return value;
}
