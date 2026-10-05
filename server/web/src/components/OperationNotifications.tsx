import {createContext, useContext, useMemo, type ReactNode} from 'react';
import {Button, notification} from 'antd';

type Feedback = {success(title:string):void;error(title:string,description:string,retry?:()=>void,key?:string):void;close(key:string):void};
const Context=createContext<Feedback|null>(null);

// 操作与查询共用主题上下文；查询故障的去重和恢复由 QueryNotifications 处理。
export function OperationNotifications({children}:{children:ReactNode}){
 const [api,holder]=notification.useNotification({placement:'topRight',stack:{threshold:3}});
 const feedback=useMemo<Feedback>(()=>({
  success:title=>api.success({title,duration:4,role:'status'}),
  error:(title,description,retry,key)=>api.error({key:key??`operation:${title}`,title,description,duration:0,role:'alert',actions:retry?<Button size="small" onClick={retry}>重试</Button>:undefined}),
  close:key=>api.destroy(key),
 }),[api]);
 return <Context.Provider value={feedback}>{holder}{children}</Context.Provider>;
}
export function useOperationNotifications(){
 const value=useContext(Context);
 if(!value)throw new Error('OperationNotifications provider required');
 return value;
}
