import { useEffect, useRef, type ReactNode } from 'react';
import { Grid, Splitter } from 'antd';

/** Desktop panes stay visible; compact screens keep the list mounted while reading details. */
export function RecordWorkspace({ list, detail, selectedId, listLabel, detailLabel }: {
  list: ReactNode; detail: ReactNode; selectedId?: string; listLabel: string; detailLabel: string;
}) {
  const screens = Grid.useBreakpoint();
  const compact = !screens.lg;
  const detailRef = useRef<HTMLElement>(null);
  useEffect(() => {
    if (compact && selectedId) detailRef.current?.querySelector<HTMLButtonElement>('.record-back')?.focus();
  }, [compact, selectedId]);
  const listPane = <section className="record-pane record-list-pane" aria-label={listLabel} hidden={compact && !!selectedId}>{list}</section>;
  const detailPane = <section ref={detailRef} className="record-pane record-detail-pane" aria-label={detailLabel} hidden={compact && !selectedId}>{detail}</section>;
  return <div className={`record-workspace${compact ? ' record-workspace-compact' : ''}`}>
    {compact ? <>{listPane}{detailPane}</> : <Splitter>
      <Splitter.Panel defaultSize="34%" min={280} max="55%">{listPane}</Splitter.Panel>
      <Splitter.Panel min={360}>{detailPane}</Splitter.Panel>
    </Splitter>}
  </div>;
}
