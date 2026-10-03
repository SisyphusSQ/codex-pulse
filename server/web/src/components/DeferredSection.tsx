import { Suspense, useEffect, useRef, useState, type ReactNode } from 'react';
import { Button, Card, Skeleton } from 'antd';

export function SectionPlaceholder({ title, height, activate }: { title: string; height: number; activate?: () => void }) {
  return <Card title={title} style={{ minHeight: height }}>
    <Skeleton active={!activate} paragraph={{ rows: 3 }} />
    {activate ? <Button onClick={activate}>加载{title}</Button> : <span role="status">正在读取{title}…</span>}
  </Card>;
}

// 首次接近视口后保持挂载，复用查询缓存及区块内的用户选择。
export function DeferredSection({ title, height, children }: { title: string; height: number; children: ReactNode }) {
  const target = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState(false);
  useEffect(() => {
    if (active) return;
    if (typeof IntersectionObserver === 'undefined') { setActive(true); return; }
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) { setActive(true); observer.disconnect(); }
    }, { rootMargin: '300px 0px' });
    if (target.current) observer.observe(target.current);
    return () => observer.disconnect();
  }, [active]);
  return <div ref={target} className="deferred-section" aria-label={title}>
    {active ? <Suspense fallback={<SectionPlaceholder title={title} height={height} />}>{children}</Suspense>
      : <SectionPlaceholder title={title} height={height} activate={() => setActive(true)} />}
  </div>;
}
