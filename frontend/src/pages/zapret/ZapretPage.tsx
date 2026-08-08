import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  ConfigProvider,
  Form,
  Input,
  Layout,
  Result,
  Row,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd';
import { CheckCircleOutlined, CloseCircleOutlined, DownloadOutlined, PlayCircleOutlined, PoweroffOutlined, RedoOutlined, SaveOutlined } from '@ant-design/icons';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { useZapretStatus, useZapretHosts, useZapretMutations } from '@/api/queries/useSystemTools';

const { Text, Title } = Typography;

const ZAPRET_DOWNLOAD_URL = 'https://github.com/ImMALWARE/zapret-linux-easy/archive/refs/heads/main.zip';

export default function ZapretPage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => { setMessageInstance(messageApi); }, [messageApi]);
  usePageTitle();

  const { data: status, isLoading, isError, refetch } = useZapretStatus();
  const { data: hosts, refetch: refetchHosts } = useZapretHosts();
  const { install, downloadInstall, uninstall, start, stop, restart, saveHosts } = useZapretMutations();
  const [form] = Form.useForm<{ firewall: string; ifaceWan: string; ifaceLan: string }>();
  const [dlForm] = Form.useForm<{ firewall: string; ifaceWan: string; ifaceLan: string }>();
  const [bypassText, setBypassText] = useState('');
  const [ignoreText, setIgnoreText] = useState('');
  const [busy, setBusy] = useState<string | null>(null);

  useEffect(() => {
    if (hosts) {
      setBypassText(hosts.bypass.join('\n'));
      setIgnoreText(hosts.ignore.join('\n'));
    }
  }, [hosts]);

  const pageClass = useMemo(() => {
    const c = ['zapret-page'];
    if (isDark) c.push('is-dark');
    if (isUltra) c.push('is-ultra');
    return c.join(' ');
  }, [isDark, isUltra]);

  const run = async (kind: 'install' | 'uninstall' | 'start' | 'stop' | 'restart') => {
    setBusy(kind);
    try {
      if (kind === 'install') {
        const values = await form.validateFields();
        await install(values);
      } else if (kind === 'uninstall') await uninstall();
      else if (kind === 'start') await start();
      else if (kind === 'stop') await stop();
      else await restart();
    } finally {
      setBusy(null);
      await refetch();
      await refetchHosts();
    }
  };

  const runDownload = async () => {
    const values = await dlForm.validateFields();
    setBusy('download');
    try {
      const cfg = { url: ZAPRET_DOWNLOAD_URL, firewall: values.firewall || 'nftables', ifaceWan: values.ifaceWan ?? '', ifaceLan: values.ifaceLan ?? '' };
      await downloadInstall(cfg);
    } finally {
      setBusy(null);
      await refetch();
      await refetchHosts();
      dlForm.resetFields();
    }
  };

  const runSaveHosts = async () => {
    setBusy('hosts');
    try {
      await saveHosts({
        bypass: bypassText.split('\n').map((s) => s.trim()).filter(Boolean),
        ignore: ignoreText.split('\n').map((s) => s.trim()).filter(Boolean),
      });
    } finally {
      setBusy(null);
      await refetchHosts();
    }
  };

  const tag = (label: React.ReactNode, ok: boolean) => (
    <Tag color={ok ? 'green' : 'red'} icon={ok ? <CheckCircleOutlined /> : <CloseCircleOutlined />}>{label}</Tag>
  );

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin spinning={isLoading}>
              <Title level={3}>{t('menu.zapret')}</Title>
              <Text type="secondary" style={{ display: 'block', marginBottom: 24 }}>{t('pages.zapret.desc')}</Text>

              <Card title={t('pages.zapret.statusCard')} variant="borderless" style={{ marginBottom: 16 }}>
                {isError ? <Result status="error" title={t('pages.zapret.installFailed')} /> : !status ? null : (
                  <Space wrap size="large">
                    {tag(t('pages.zapret.installed'), status.installed)}
                    {status.installed && tag(t('pages.zapret.running'), status.running)}
                    {status.firewall && <Text>{t('pages.zapret.firewall')}: <Text strong>{status.firewall}</Text></Text>}
                  </Space>
                )}
              </Card>

              {!status?.installed ? (
                <>
                  <Card title={t('pages.zapret.installTitle')} variant="borderless" style={{ marginBottom: 16 }}>
                  <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>{t('pages.zapret.notInstalled')}</Text>
                  <Form form={form} layout="vertical" initialValues={{ firewall: 'nftables' }} style={{ maxWidth: 480 }}>
                    <Form.Item name="firewall" label={t('pages.zapret.firewallLabel')}>
                      <Select
                        options={[
                          { value: 'nftables', label: 'nftables' },
                          { value: 'iptables', label: 'iptables' },
                        ]}
                      />
                    </Form.Item>
                    <Form.Item name="ifaceWan" label={t('pages.zapret.ifaceWan')}>
                      <Input placeholder="eth0" />
                    </Form.Item>
                    <Form.Item name="ifaceLan" label={t('pages.zapret.ifaceLan')}>
                      <Input placeholder="" />
                    </Form.Item>
<Button type="primary" icon={<CheckCircleOutlined />} loading={busy === 'install'} onClick={() => void run('install')}>
                      {t('pages.zapret.installBtn')}
                    </Button>
                  </Form>
                </Card>
                <Card title={t('pages.zapret.downloadTitle')} variant="borderless" style={{ marginBottom: 16 }}>
                  <Form form={dlForm} layout="vertical" initialValues={{ firewall: 'nftables' }}>
                    <Form.Item label={t('pages.zapret.downloadUrl')}>
                      <Input value={ZAPRET_DOWNLOAD_URL} readOnly style={{ fontFamily: 'monospace' }} />
                    </Form.Item>
                    <Form.Item name="firewall" label={t('pages.zapret.firewallLabel')}>
                      <Select
                        options={[
                          { value: 'nftables', label: 'nftables' },
                          { value: 'iptables', label: 'iptables' },
                        ]}
                      />
                    </Form.Item>
                    <Form.Item name="ifaceWan" label={t('pages.zapret.ifaceWan')}>
                      <Input placeholder="eth0" />
                    </Form.Item>
                    <Form.Item name="ifaceLan" label={t('pages.zapret.ifaceLan')}>
                      <Input placeholder="" />
                    </Form.Item>
                    <Button type="primary" icon={<DownloadOutlined />} loading={busy === 'download'} onClick={() => void runDownload()}>
                      {t('pages.zapret.downloadBtn')}
                    </Button>
                  </Form>
                </Card>
                </>
              ) : (
                <>
                  <Card
                    title={t('pages.zapret.hostsTitle')}
                    variant="borderless"
                    style={{ marginBottom: 16 }}
                    extra={
                      <Space>
                        <Button icon={<RedoOutlined />} loading={busy === 'restart'} onClick={() => void run('restart')}>{t('pages.zapret.restartBtn')}</Button>
                        <Button danger icon={<PoweroffOutlined />} loading={busy === 'stop'} onClick={() => void run('stop')}>{t('pages.zapret.stopBtn')}</Button>
                        <Button icon={<PlayCircleOutlined />} loading={busy === 'start'} onClick={() => void run('start')}>{t('pages.zapret.startBtn')}</Button>
                      </Space>
                    }
                  >
                    <Row gutter={16}>
                      <Col xs={24} md={12}>
                        <Text strong>{t('pages.zapret.bypassLabel')}</Text>
                        <Input.TextArea rows={10} value={bypassText} onChange={(e) => setBypassText(e.target.value)} placeholder="example.com" />
                      </Col>
                      <Col xs={24} md={12}>
                        <Text strong>{t('pages.zapret.ignoreLabel')}</Text>
                        <Input.TextArea rows={10} value={ignoreText} onChange={(e) => setIgnoreText(e.target.value)} />
                      </Col>
                    </Row>
                    <Button type="primary" icon={<SaveOutlined />} loading={busy === 'hosts'} onClick={() => void runSaveHosts()} style={{ marginTop: 16 }}>
                      {t('pages.zapret.saveHosts')}
                    </Button>
                  </Card>
                  <Card title={t('pages.zapret.uninstallBtn')} variant="borderless">
                    <Button danger loading={busy === 'uninstall'} onClick={() => void run('uninstall')}>{t('pages.zapret.uninstallBtn')}</Button>
                  </Card>
                </>
              )}
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}