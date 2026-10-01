import { useEffect, useRef } from 'react';
import { init, use, type EChartsCoreOption } from 'echarts/core';
import { BarChart, LineChart, HeatmapChart } from 'echarts/charts';
import { GridComponent, TooltipComponent, CalendarComponent, VisualMapComponent, LegendComponent } from 'echarts/components';
import { SVGRenderer } from 'echarts/renderers';

use([BarChart,LineChart,HeatmapChart,GridComponent,TooltipComponent,CalendarComponent,VisualMapComponent,LegendComponent,SVGRenderer]);

export default function Chart({ option, label, height=280 }: { option: EChartsCoreOption; label: string; height?: number }) {
  const element=useRef<HTMLDivElement>(null);
  useEffect(()=>{
    if(!element.current) return;
    const chart=init(element.current,undefined,{renderer:'svg'});
    chart.setOption(option);
    const observer=new ResizeObserver(()=>chart.resize());
    observer.observe(element.current);
    return ()=>{observer.disconnect();chart.dispose();};
  },[option]);
  return <div ref={element} role="img" aria-label={label} style={{height,width:'100%'}} />;
}
