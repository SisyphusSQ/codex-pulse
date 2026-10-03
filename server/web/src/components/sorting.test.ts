import {describe,it,expect} from 'vitest';
import {decimalCompare,decimalSorter} from './sorting';
describe('table sorting',()=>{
 it('compares raw huge integers and fractional prices exactly',()=>{
  expect(decimalCompare('9007199254740993','9007199254740992')).toBe(1);
  expect(decimalCompare('1.01','1.001')).toBe(1);expect(decimalCompare('-1.01','-1.001')).toBe(-1);
 });
 it('defaults descending and keeps unknown behind known zero in both directions',()=>{
  const column=decimalSorter<{value:string|null}>(r=>r.value,true);
  expect(column.defaultSortOrder).toBe('descend');
  const compare=column.sorter as (a:{value:string|null},b:{value:string|null},order:'ascend'|'descend')=>number;
  for(const order of ['ascend','descend'] as const){const sign=order==='descend'?-1:1;const values=[{value:null},{value:'0'},{value:'9007199254740993'},{value:'9007199254740992'}].sort((a,b)=>sign*compare(a,b,order));expect(values.at(-1)?.value).toBeNull();}
 });
});
