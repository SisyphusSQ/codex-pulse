import { expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { Device } from '../api/statistics';
import { SourceTable } from './SourceTable';

it('shows actual device evidence with original cutoff, revocation and unknown values without claiming execution usage',()=>{
  const device:Device={id:'synthetic',name:'<img src=x onerror=alert(1)>',revoked_at_ms:1,last_received_at_ms:2,providers:[{provider:'codex',version:'test',collected_at_ms:null,coverage_start_ms:null,coverage_end_ms:null,pending_batches:0,status:'ready',received_at_ms:2,stale:true}]};
  render(<SourceTable devices={[device]} />);
  expect(screen.getByText(device.name)).toBeInTheDocument();expect(document.querySelector('img[src="x"]')).toBeNull();
  expect(screen.getByText('已撤销')).toBeInTheDocument();expect(screen.getByText('陈旧')).toBeInTheDocument();expect(screen.getByText('尚无观测')).toBeInTheDocument();
  expect(screen.queryByText('正常')).not.toBeInTheDocument();expect(screen.queryByText('会话数')).not.toBeInTheDocument();
});
