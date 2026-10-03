import type {ColumnType} from 'antd/es/table';
// 排序使用原始整数，保留超出 JS 安全整数范围的精度；未知始终置后。
export function decimalCompare(a:string|number|null|undefined,b:string|number|null|undefined):number {
 if(a==null||b==null)return a==null?(b==null?0:-1):1;
 const parts=(v:string|number)=>String(v).split('.');
 const [ai,af='']=parts(a),[bi,bf='']=parts(b),scale=Math.max(af.length,bf.length);
 const exact=(whole:string,fraction:string)=>BigInt(`${whole.startsWith('-')?'-':''}${whole.replace(/^-/, '')}${fraction.padEnd(scale,'0')}`);
 const left=exact(ai,af),right=exact(bi,bf);return left<right?-1:left>right?1:0;
}
export function decimalSorter<T>(value:(row:T)=>string|number|null|undefined,primary=false):Pick<ColumnType<T>,'sorter'|'defaultSortOrder'|'sortDirections'>{
 return {sorter:(a,b,order)=>{const left=value(a),right=value(b);if(left==null||right==null){const missing=left==null?(right==null?0:1):-1;return order==='descend'?-missing:missing;}return decimalCompare(left,right);},defaultSortOrder:primary?'descend':undefined,sortDirections:['descend','ascend','descend']};
}
