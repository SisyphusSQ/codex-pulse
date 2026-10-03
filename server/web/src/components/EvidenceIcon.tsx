import type {ReactNode} from 'react';
import {Button,Popover} from 'antd';
import {ExclamationCircleOutlined,InfoCircleOutlined} from '@ant-design/icons';
export function EvidenceIcon({label,warning=false,children}:{label:string;warning?:boolean;children:ReactNode}){
 return <Popover trigger={['hover','click','focus']} title={label} content={<div className="evidence-explanation">{children}</div>}><Button type="text" size="small" aria-label={label} className={warning?'warning-icon':'info-icon'} icon={warning?<ExclamationCircleOutlined />:<InfoCircleOutlined />} /></Popover>;
}
