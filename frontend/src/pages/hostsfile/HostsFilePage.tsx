import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  ConfigProvider,
  Form,
  Input,
  InputNumber,
  Layout,
  Modal,
  Result,
  Row,
  Space,
  Spin,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  DownloadOutlined,
  FileTextOutlined,
  PlayCircleOutlined,
  SaveOutlined,
  SyncOutlined,
  ThunderboltOutlined,
  UndoOutlined,
} from '@ant-design/icons';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import {
  useHostsFile,
  useHostsFileMutations,
  useDnsResolverStatus,
  useDnsResolverMutations,
  type DnsResolverEntry,
  type DnsResolverSettings,
  type DnsTestResult,
} from '@/api/queries/useSystemTools';

const { Text, Title } = Typography;

// ─── Hosts File Tab ──────────────────────────────────────────────────────────

function HostsFileTab() {
  const { t } = useTranslation();
  const { data, isLoading, isError, refetch } = useHostsFile();
  const { save, download } = useHostsFileMutations();
  const [content, setContent] = useState('');
  const [url, setUrl] = useState('');
  const [busy, setBusy] = useState(false);
  const [syncedFrom, setSyncedFrom] = useState<typeof data>(undefined);

  if (data && syncedFrom !== data) {
    setSyncedFrom(data);
    setContent(data.raw);
  }

  const columns: TableColumnsType<{ ip: string; domain: string }> = [
    {
      title: <Text type="secondary">{t('pages.hostsfile.colIp')}</Text>,
      dataIndex: 'ip',
      key: 'ip',
    },
    {
      title: <Text type="secondary">{t('pages.hostsfile.colDomain')}</Text>,
      dataIndex: 'domain',
      key: 'domain',
    },
  ];

  const runSave = async () => {
    setBusy(true);
    try {
      await save(content);
    } finally {
      setBusy(false);
      await refetch();
    }
  };

  const runDownload = async () => {
    setBusy(true);
    try {
      await download(url);
    } finally {
      setBusy(false);
      setUrl('');
      await refetch();
    }
  };

  if (isError) {
    return <Result status="error" title={t('pages.hostsfile.getFailed')} />;
  }

  return (
    <Spin spinning={isLoading}>
      <Card
        title={t('pages.hostsfile.editorTitle')}
        variant="borderless"
        style={{ marginBottom: 16 }}
      >
        <Input.TextArea
          rows={14}
          value={content}
          onChange={(e) => setContent(e.target.value)}
          spellCheck={false}
          style={{ fontFamily: 'monospace' }}
        />
        <Button
          type="primary"
          icon={<SaveOutlined />}
          loading={busy}
          onClick={() => void runSave()}
          style={{ marginTop: 16 }}
        >
          {t('pages.hostsfile.saveBtn')}
        </Button>
      </Card>
      <Card
        title={t('pages.hostsfile.downloadTitle')}
        variant="borderless"
        style={{ marginBottom: 16 }}
      >
        <Space.Compact style={{ width: '100%' }}>
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder={t('pages.hostsfile.downloadPlaceholder')}
            style={{ fontFamily: 'monospace' }}
          />
          <Button
            icon={<DownloadOutlined />}
            loading={busy}
            disabled={!url}
            onClick={() => void runDownload()}
          >
            {t('pages.hostsfile.downloadBtn')}
          </Button>
        </Space.Compact>
      </Card>
      {data && data.entries.length > 0 && (
        <Card title={t('pages.hostsfile.entriesTitle')} variant="borderless">
          <Table
            rowKey={(r) => `${r.ip} ${r.domain}`}
            columns={columns}
            dataSource={data.entries}
            size="small"
          />
        </Card>
      )}
    </Spin>
  );
}

// ─── DNS Resolver Tab ────────────────────────────────────────────────────────

type MessageApi = ReturnType<typeof message.useMessage>[0];

function DnsResolverTab({ messageApi }: { messageApi: MessageApi }) {
  const { t } = useTranslation();
  const [busy, setBusy] = useState<string | null>(null);
  const [domainsModalOpen, setDomainsModalOpen] = useState(false);
  const [domainsText, setDomainsText] = useState('');
  const [settingsForm] = Form.useForm<{ dnsServer: string; intervalHours: number }>();

  const { data: status, isLoading, refetch } = useDnsResolverStatus(3000);
  const { saveSettings, saveDomains, resolve, testDns } = useDnsResolverMutations();
  const [testResult, setTestResult] = useState<DnsTestResult | null>(null);
  const [testingDns, setTestingDns] = useState(false);

  useEffect(() => {
    if (!status) return;
    // The status endpoint is polled; never overwrite values the operator is
    // editing, especially after disabling auto-update.
    if (settingsForm.isFieldsTouched()) return;
    settingsForm.setFieldsValue({
      dnsServer: status.dnsServer,
      intervalHours: status.intervalHours,
    });
  }, [status, settingsForm]);

  const onTestDns = async () => {
    const srv = (settingsForm.getFieldValue('dnsServer') as string | undefined)?.trim();
    setTestingDns(true);
    setTestResult(null);
    try {
      const res = await testDns(srv || '');
      const data = res.obj ?? {
        success: res.success,
        latencyMs: 0,
        server: srv || '',
        error: res.msg || 'Error',
      };
      setTestResult(data);
      if (res.success && data.success) {
        messageApi.success(t('pages.dnsresolver.testSuccess', { latency: data.latencyMs }));
      } else {
        messageApi.error(data.error || t('pages.dnsresolver.testFailed'));
      }
    } catch (err: unknown) {
      const e = err as { message?: string };
      setTestResult({
        success: false,
        latencyMs: 0,
        server: srv || '',
        error: e.message || 'Error',
      });
      messageApi.error(e.message || t('pages.dnsresolver.testFailed'));
    } finally {
      setTestingDns(false);
    }
  };

  const onResetToDefaultDomains = () => {
    if (status?.defaultDomains && status.defaultDomains.length > 0) {
      setDomainsText(status.defaultDomains.join('\n'));
    }
  };

  const onToggleEnabled = async (enabled: boolean) => {
    if (!status) return;
    setBusy('toggle');
    try {
      const payload: DnsResolverSettings = {
        enabled,
        dnsServer: String(settingsForm.getFieldValue('dnsServer') ?? '').trim() || status.dnsServer,
        intervalHours: Number(settingsForm.getFieldValue('intervalHours')) || status.intervalHours,
        domains: status.domains,
      };
      await saveSettings(payload);
    } finally {
      setBusy(null);
      await refetch();
    }
  };

  const onSaveSettings = async () => {
    try {
      const values = await settingsForm.validateFields();
      setBusy('settings');
      try {
        const payload: DnsResolverSettings = {
          enabled: status?.enabled ?? false,
          dnsServer: values.dnsServer.trim(),
          intervalHours: Number(values.intervalHours),
          domains: status?.domains ?? [],
        };
        await saveSettings(payload);
      } finally {
        setBusy(null);
        await refetch();
      }
    } catch {
      // validation error
    }
  };

  const onOpenDomainsModal = () => {
    setDomainsText((status?.domains ?? []).join('\n'));
    setDomainsModalOpen(true);
  };

  const onSaveDomains = async () => {
    setBusy('domains');
    try {
      const parsed = domainsText
        .split('\n')
        .map((s) => s.trim())
        .filter((s) => s.length > 0 && !s.startsWith('#'));
      await saveDomains(parsed);
      setDomainsModalOpen(false);
    } finally {
      setBusy(null);
      await refetch();
    }
  };

  const onResolveNow = async () => {
    setBusy('resolve');
    try {
      await resolve();
    } finally {
      setBusy(null);
      await refetch();
    }
  };

  const entryColumns: TableColumnsType<DnsResolverEntry> = [
    {
      title: <Text type="secondary">{t('pages.dnsresolver.colIp')}</Text>,
      dataIndex: 'ip',
      key: 'ip',
      render: (ip: string) =>
        ip ? <Tag color="geekblue">{ip}</Tag> : <Text type="secondary">—</Text>,
    },
    {
      title: <Text type="secondary">{t('pages.dnsresolver.colDomain')}</Text>,
      dataIndex: 'domain',
      key: 'domain',
      render: (domain: string) => <Text strong>{domain}</Text>,
    },
    {
      title: <Text type="secondary">{t('pages.dnsresolver.colStatus')}</Text>,
      dataIndex: 'status',
      key: 'status',
      render: (st: string) =>
        st === 'OK' ? (
          <Tag color="green" icon={<CheckCircleOutlined />}>
            OK
          </Tag>
        ) : (
          <Tag color="red" icon={<CloseCircleOutlined />}>
            {st}
          </Tag>
        ),
    },
  ];

  return (
    <Spin spinning={isLoading}>
      {/* Status Card */}
      <Card
        title={t('pages.dnsresolver.statusCard')}
        variant="borderless"
        style={{ marginBottom: 16 }}
      >
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Space wrap size="large" align="center">
            <Space align="center">
              <Text strong>{t('pages.dnsresolver.autoUpdateLabel')}:</Text>
              <Switch
                checked={status?.enabled ?? false}
                loading={busy === 'toggle'}
                onChange={(checked) => void onToggleEnabled(checked)}
              />
            </Space>
            {status?.running ? (
              <Tag icon={<SyncOutlined spin />} color="processing">
                {t('pages.dnsresolver.statusRunning')}
              </Tag>
            ) : status?.enabled ? (
              <Tag color="green" icon={<CheckCircleOutlined />}>
                {t('pages.dnsresolver.statusActive')}
              </Tag>
            ) : (
              <Tag color="default" icon={<CloseCircleOutlined />}>
                {t('pages.dnsresolver.statusDisabled')}
              </Tag>
            )}
            <Tag color="blue">
              {t('pages.dnsresolver.domainsCount')}: {status?.domains?.length ?? 0}
            </Tag>
            {status?.lastRun && (
              <Tag color="purple">
                {t('pages.dnsresolver.lastUpdate')}: {status.lastRun}
              </Tag>
            )}
            {status?.lastResult && (
              <Tag color={status.lastError ? 'orange' : 'cyan'}>{status.lastResult}</Tag>
            )}
          </Space>

          <Space wrap>
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              loading={busy === 'resolve' || status?.running}
              onClick={() => void onResolveNow()}
            >
              {t('pages.dnsresolver.resolveNowBtn')}
            </Button>
            <Button icon={<FileTextOutlined />} onClick={onOpenDomainsModal}>
              {t('pages.dnsresolver.changeDomainsBtn')}
            </Button>
          </Space>
        </Space>
      </Card>

      {/* Settings Card */}
      <Card
        title={t('pages.dnsresolver.settingsCard')}
        variant="borderless"
        style={{ marginBottom: 16 }}
      >
        <Form form={settingsForm} layout="vertical">
          <Row gutter={[16, 16]}>
            <Col xs={24} md={16}>
              <Form.Item
                name="dnsServer"
                label={t('pages.dnsresolver.dnsServerLabel')}
                extra={
                  <Space direction="vertical" size={2} style={{ marginTop: 4 }}>
                    <Text type="secondary">{t('pages.dnsresolver.dnsServerHint')}</Text>
                    {testResult && (
                      <div style={{ marginTop: 4 }}>
                        {testResult.success ? (
                          <Tag color="success" icon={<CheckCircleOutlined />}>
                            {t('pages.dnsresolver.testSuccess', { latency: testResult.latencyMs })}
                            {testResult.ip ? ` (${testResult.ip})` : ''}
                          </Tag>
                        ) : (
                          <Tag color="error" icon={<CloseCircleOutlined />}>
                            {t('pages.dnsresolver.testFailed')}: {testResult.error}
                          </Tag>
                        )}
                      </div>
                    )}
                  </Space>
                }
                rules={[{ required: true }]}
              >
                <Space.Compact style={{ width: '100%' }}>
                  <Input placeholder="https://dns.comss.one/dns-query" />
                  <Button
                    icon={<ThunderboltOutlined />}
                    loading={testingDns}
                    onClick={() => void onTestDns()}
                  >
                    {t('pages.dnsresolver.testDnsBtn')}
                  </Button>
                </Space.Compact>
              </Form.Item>
            </Col>
            <Col xs={24} md={8}>
              <Form.Item
                name="intervalHours"
                label={t('pages.dnsresolver.intervalLabel')}
                extra={t('pages.dnsresolver.intervalHint')}
                rules={[{ required: true }]}
              >
                <InputNumber min={1} max={168} placeholder="12" style={{ width: '100%' }} />
              </Form.Item>
            </Col>
          </Row>
          <Button
            type="primary"
            icon={<SaveOutlined />}
            loading={busy === 'settings'}
            onClick={() => void onSaveSettings()}
          >
            {t('pages.dnsresolver.saveSettingsBtn')}
          </Button>
        </Form>
      </Card>

      {/* Resolved Entries Table */}
      {status && status.entries && status.entries.length > 0 && (
        <Card title={t('pages.dnsresolver.resolvedDomainsTable')} variant="borderless">
          <Table
            rowKey={(r) => r.domain}
            columns={entryColumns}
            dataSource={status.entries}
            size="small"
            pagination={{ pageSize: 15, showSizeChanger: true }}
          />
        </Card>
      )}

      {/* Modal: Edit domains for resolve (Zapret lists style) */}
      <Modal
        open={domainsModalOpen}
        title={t('pages.dnsresolver.changeDomainsTitle')}
        onOk={() => void onSaveDomains()}
        onCancel={() => setDomainsModalOpen(false)}
        okText={t('save')}
        cancelText={t('cancel')}
        confirmLoading={busy === 'domains'}
        width={600}
      >
        <Text type="secondary" style={{ display: 'block', marginBottom: 12 }}>
          {t('pages.dnsresolver.changeDomainsHint')}
        </Text>
        <Input.TextArea
          rows={14}
          value={domainsText}
          onChange={(e) => setDomainsText(e.target.value)}
          placeholder={`chatgpt.com\napi.openai.com\nclaude.ai\ngemini.google.com\naistudio.google.com`}
          style={{ fontFamily: 'monospace', whiteSpace: 'pre' }}
          spellCheck={false}
        />
        <div
          style={{
            marginTop: 8,
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
          }}
        >
          <Button size="small" icon={<UndoOutlined />} onClick={onResetToDefaultDomains}>
            {t('pages.dnsresolver.resetDefaultsBtn')}
          </Button>
          <Text type="secondary">
            {t('pages.dnsresolver.domainsCount')}:{' '}
            {
              domainsText
                .split('\n')
                .filter((s) => s.trim().length > 0 && !s.trim().startsWith('#')).length
            }
          </Text>
        </div>
      </Modal>
    </Spin>
  );
}

// ─── Main Page Shell ─────────────────────────────────────────────────────────

type TabKey = 'hostsfile' | 'dnsresolver';

export default function HostsFilePage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);
  usePageTitle();

  const [activeTab, setActiveTab] = useState<TabKey>('hostsfile');

  const pageClass = useMemo(() => {
    const c = ['hostsfile-page'];
    if (isDark) c.push('is-dark');
    if (isUltra) c.push('is-ultra');
    return c.join(' ');
  }, [isDark, isUltra]);

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Title level={3}>{t('menu.hostsFile')}</Title>
            <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
              {activeTab === 'hostsfile' ? t('pages.hostsfile.desc') : t('pages.dnsresolver.desc')}
            </Text>
            <Tabs
              activeKey={activeTab}
              onChange={(k) => setActiveTab(k as TabKey)}
              items={[
                { key: 'hostsfile', label: t('pages.hostsfile.tabHosts') },
                { key: 'dnsresolver', label: t('pages.dnsresolver.tabDnsResolver') },
              ]}
              style={{ marginBottom: 16 }}
            />
            {activeTab === 'hostsfile' ? (
              <HostsFileTab />
            ) : (
              <DnsResolverTab messageApi={messageApi} />
            )}
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
