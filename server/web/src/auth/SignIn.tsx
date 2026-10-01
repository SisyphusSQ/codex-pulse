import { useState } from 'react';
import { Alert, Button, Card, Form, Input, Typography } from 'antd';
import { ApiError } from '../api/client';
import { useSession } from './SessionProvider';

export function SignIn() {
  const { signIn } = useSession();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [form] = Form.useForm<{ code: string }>();

  async function submit({ code }: { code: string }) {
    setBusy(true);
    setError(undefined);
    try {
      await signIn(code.trim());
      form.resetFields();
    } catch (cause) { setError(cause instanceof ApiError ? cause.message : '配对未完成，请稍后重试。'); }
    finally { setBusy(false); }
  }

  return <main className="sign-in">
    <div className="sign-in-intro">
      <div className="eyebrow">CODEX PULSE · 多机中心</div>
      <Typography.Title>让每台机器的用量，<br />汇到同一个视野。</Typography.Title>
      <Typography.Paragraph type="secondary">统计、项目、会话与账号额度，在这里集中查看。采集由本机 App 管理，上报由你开启。</Typography.Paragraph>
    </div>
    <Card className="sign-in-card" title="授权此浏览器">
      <Typography.Paragraph type="secondary">输入管理员签发的浏览器配对码，完成一次性授权。</Typography.Paragraph>
      {error && <Alert type="error" title={error} showIcon className="form-alert" />}
      <Form form={form} layout="vertical" onFinish={submit} autoComplete="off" disabled={busy}>
        <Form.Item name="code" label="浏览器配对码" rules={[{ required: true, message: '请输入配对码。' }, { max: 64, message: '请检查配对码。' }]}>
          <Input placeholder="XXXX-XXXX-XXXX-XXXX" maxLength={64} spellCheck={false} autoComplete="off" autoCapitalize="characters" />
        </Form.Item>
        <Button color="primary" variant="solid" htmlType="submit" loading={busy} block>配对并进入</Button>
      </Form>
      <Typography.Paragraph type="secondary" className="sign-in-note">配对码短期有效，只能使用一次。失效后请申请新的浏览器配对码。</Typography.Paragraph>
    </Card>
  </main>;
}
