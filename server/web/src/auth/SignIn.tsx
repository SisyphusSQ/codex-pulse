import { useState } from 'react';
import { Button, Card, Form, Input, Typography } from 'antd';
import { SafetyCertificateOutlined } from '@ant-design/icons';
import { ApiError } from '../api/client';
import { useSession } from './SessionProvider';
import { useOperationNotifications } from '../components/OperationNotifications';

export function SignIn() {
  const { signIn } = useSession();
  const [busy, setBusy] = useState(false);
  const notify = useOperationNotifications();
  const [form] = Form.useForm<{ code: string }>();

  async function submit({ code }: { code: string }) {
    setBusy(true);
    notify.close('sign-in');
    try {
      await signIn(code.trim());
      form.resetFields();
    } catch (cause) { notify.error('浏览器配对未完成', cause instanceof ApiError ? cause.message : '配对未完成，请稍后重试。', undefined, 'sign-in'); }
    finally { setBusy(false); }
  }

  return <main className="sign-in">
    <div className="sign-in-intro"><SafetyCertificateOutlined /><Typography.Title level={2}>Codex Pulse</Typography.Title></div>
    <Card className="sign-in-card" title="浏览器授权">
      <Typography.Paragraph type="secondary">输入管理员签发的浏览器配对码，完成一次性授权。</Typography.Paragraph>
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
