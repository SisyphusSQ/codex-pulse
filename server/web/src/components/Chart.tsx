import { useEffect, useRef } from 'react';
import { init, use, type EChartsCoreOption } from 'echarts/core';
import { BarChart, LineChart, HeatmapChart, PieChart } from 'echarts/charts';
import { GridComponent, TooltipComponent, CalendarComponent, VisualMapComponent, LegendComponent } from 'echarts/components';
import { SVGRenderer } from 'echarts/renderers';

use([BarChart,LineChart,HeatmapChart,PieChart,GridComponent,TooltipComponent,CalendarComponent,VisualMapComponent,LegendComponent,SVGRenderer]);

export default function Chart({ option, label, height=280, onClick }: { onClick?:(index:number)=>void; option: EChartsCoreOption; label: string; height?: number }) {
  const element=useRef<HTMLDivElement>(null);
  useEffect(()=>{
    if(!element.current) return;
    const chart=init(element.current,undefined,{renderer:'svg'});
    chart.setOption(option);
    if(onClick)chart.on('click',params=>onClick(params.dataIndex));
    const observer=new ResizeObserver(()=>chart.resize());
    observer.observe(element.current);
    return ()=>{observer.disconnect();chart.dispose();};
  },[option,onClick]);
  return <div ref={element} role="img" aria-label={label} style={{height,width:'100%'}} />;
}
