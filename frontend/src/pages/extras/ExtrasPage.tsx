import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  App,
  Button,
  Card,
  Col,
  Collapse,
  ConfigProvider,
  Form,
  Input,
  InputNumber,
  Layout,
  Modal,
  QRCode,
  Result,
  Row,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
  Upload,
  message,
} from 'antd';
import type { UploadProps } from 'antd';
import { PlusOutlined, PoweroffOutlined, QrcodeOutlined, RedoOutlined, RobotOutlined, SettingOutlined, UploadOutlined, DeleteOutlined, CopyOutlined, InfoCircleOutlined, EditOutlined } from '@ant-design/icons';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { LazyMount } from '@/components/utility';
import { useExtrasStatus, useExtrasMutations, type CoreName, type ExtraConfig, type WDTTClient } from '@/api/queries/useSystemTools';

const { Text, Title } = Typography;

function ServiceCard({ name, displayName, isDark, isUltra }: { name: CoreName; displayName: string; isDark: boolean; isUltra: boolean }) {
  const { t } = useTranslation();
  const { modal } = App.useApp();
  const { data: services, isLoading, isError, error } = useExtrasStatus();
  const { start, stop, restart, saveConfig, uploadBinary, deleteBinary } = useExtrasMutations();
  const [editOpen, setEditOpen] = useState(false);
  const [form] = Form.useForm<ExtraConfig>();
  const [busy, setBusy] = useState<string | null>(null);

  const provider = Form.useWatch('provider', form);
  const transport = Form.useWatch('transport', form);

  useEffect(() => {
    if (provider === 'telemost' && transport !== 'vp8channel') {
      form.setFieldValue('transport', 'vp8channel');
    }
  }, [provider, transport, form]);

  const service = services?.find((s) => s.name === name);

  const subLink = useMemo(() => {
    if (name !== 'qwdtt' || !service?.config?.subToken) return null;
    const token = service.config.subToken;
    const base = window.X_UI_BASE_PATH?.replace(/\/+$/, '') ?? '';
    return (subUri: string) =>
      `${window.location.origin}${base}/panel/qwdtt/sub/${encodeURIComponent(token)}/${encodeURIComponent(subUri)}`;
  }, [name, service?.config?.subToken]);

  const doAction = useCallback(async (kind: 'start' | 'stop' | 'restart') => {
    setBusy(kind);
    try {
      if (kind === 'start') await start(name);
      else if (kind === 'stop') await stop(name);
      else await restart(name);
    } finally {
      setBusy(null);
    }
  }, [name, start, stop, restart]);

  const openEdit = () => {
    form.setFieldsValue(service?.config ?? {});
    setEditOpen(true);
  };

  const onDelete = () => {
    modal.confirm({
      title: t('pages.extras.deleteConfirmTitle'),
      content: t('pages.extras.deleteConfirmContent'),
      okText: t('delete'),
      okButtonProps: { danger: true },
      cancelText: t('cancel'),
      onOk: async () => {
        setBusy('delete');
        try {
          await deleteBinary(name);
        } finally {
          setBusy(null);
        }
      },
    });
  };

  const onSave = async () => {
    const values = await form.validateFields();
    await saveConfig(name, { ...service?.config, ...values });
    setEditOpen(false);
  };

  const onUpload: UploadProps['customRequest'] = async (options) => {
    const file = options.file as File;
    if (file) {
      await uploadBinary(name, file);
    }
    options.onSuccess?.(null);
  };

  const generateKey = () => {
    const bytes = new Uint8Array(32);
    crypto.getRandomValues(bytes);
    form.setFieldValue('cryptoKey', Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join(''));
  };

  const [clientOpen, setClientOpen] = useState(false);
  const [clientIndex, setClientIndex] = useState<number | null>(null);
  const [infoClient, setInfoClient] = useState<WDTTClient | null>(null);
  const [qrClient, setQrClient] = useState<{ client: WDTTClient; index: number } | null>(null);
  const [clientForm] = Form.useForm<WDTTClient>();
  const [visiblePasswords, setVisiblePasswords] = useState<Set<number>>(new Set());

  const clients = service?.config?.clients ?? [];

  const [tgOpen, setTgOpen] = useState(false);
  const [tgForm] = Form.useForm<{ adminId: string; botToken: string }>();

  const openTg = () => {
    tgForm.setFieldsValue({
      adminId: service?.config?.adminId ?? '',
      botToken: service?.config?.botToken ?? '',
    });
    setTgOpen(true);
  };

  const onTgSave = async () => {
    const values = await tgForm.validateFields();
    const currentConfig = service?.config ?? {};
    await saveConfig(name, { ...currentConfig, adminId: values.adminId, botToken: values.botToken });
    setTgOpen(false);
  };

  const wdttLink = useCallback((cl: WDTTClient): string | null => {
    if (!cl.password) return null;
    const host = service?.config?.subHost || service?.config?.listenAddr || '';
    let ip = host.includes(':') ? host.slice(0, host.lastIndexOf(':')) : host;
    if (!ip || ip === '0.0.0.0' || ip === '::') {
      ip = window.location.hostname || '';
    }
    if (!ip) return null;
    const dtls = service?.config?.listenAddr?.split(':').pop() ?? '56000';
    const name = cl.name || 'Client';
    return `qwdtt://config?name=${encodeURIComponent(name)}&peer=${ip}:${dtls}&hashes=${cl.vkHashes || ''}&workers=16&port=9000&pass=${cl.password}`;
  }, [service?.config?.subHost, service?.config?.listenAddr]);

  // The qWDTT app's QR scanner only accepts a subscription JSON document or a
  // qwdtt://config URI — a bare URL is rejected as "wrong format". Build the
  // document exactly like the backend /sub/:token/:clientUri endpoint serves it.
  const subDoc = useCallback(
    (cl: WDTTClient): string | null => {
      if (!cl.password) return null;
      const host = service?.config?.subHost || service?.config?.listenAddr || '';
      let ip = host.includes(':') ? host.slice(0, host.lastIndexOf(':')) : host;
      if (!ip || ip === '0.0.0.0' || ip === '::') {
        ip = window.location.hostname || '';
      }
      if (!ip) return null;
      const dtls = service?.config?.listenAddr?.split(':').pop() ?? '56000';
      const doc: Record<string, unknown> = {
        subscriptionName: cl.subscriptionName || cl.name || 'Client',
        profiles: [
          {
            name: cl.name || 'Client',
            peer: `${ip}:${dtls}`,
            hashes: cl.vkHashes || '',
            workers: 16,
            port: 9000,
            password: cl.password,
          },
        ],
      };
      if (cl.subscriptionDescription) {
        doc.description = cl.subscriptionDescription;
      }
      return JSON.stringify(doc);
    },
    [service?.config?.subHost, service?.config?.listenAddr],
  );

  const openClient = (index: number | null) => {
    setClientIndex(index);
    clientForm.resetFields();
    if (index !== null) {
      clientForm.setFieldsValue(clients[index] ?? {});
    }
    setClientOpen(true);
  };

  const saveClients = async (next: WDTTClient[]) => {
    const currentConfig = service?.config ?? {};
    await saveConfig(name, { ...currentConfig, clients: next });
  };

  const onClientSave = async () => {
    const values = await clientForm.validateFields();
    const next = [...clients];
    if (clientIndex !== null) {
      next[clientIndex] = { ...clients[clientIndex], ...values };
    } else {
      next.push(values);
    }
    await saveClients(next);
    setClientOpen(false);
  };

  const onClientDelete = (index: number) => {
    modal.confirm({
      title: t('pages.extras.clientDeleteConfirm'),
      content: clients[index]?.name,
      okText: t('delete'),
      okButtonProps: { danger: true },
      cancelText: t('cancel'),
      onOk: () => saveClients(clients.filter((_, i) => i !== index)),
    });
  };

  const onClientToggle = async (index: number, enabled: boolean) => {
    const next = [...clients];
    next[index] = { ...next[index], enabled };
    await saveClients(next);
  };

  const generateClientPassword = () => {
    const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789';
    const bytes = crypto.getRandomValues(new Uint8Array(16));
    clientForm.setFieldValue('password', Array.from(bytes, (b) => chars[b % chars.length]).join(''));
  };

  if (isError) {
    return (
      <Card title={displayName} variant="borderless">
        <Result status="error" title={(error as Error)?.message ?? t('pages.extras.loadFailed')} />
      </Card>
    );
  }

  if (isLoading) {
    return <Card title={displayName} loading />;
  }
  if (!service) {
    return <Card title={displayName}><Result status="warning" title={t('pages.extras.noData')} /></Card>;
  }

  const cardClass = ['service-card', isDark ? 'is-dark' : '', isUltra ? 'is-ultra' : ''].join(' ').trim();
  const runningNow = service.running;
  const actions = [
    <Tooltip key="startstop" title={runningNow ? t('pages.extras.actions.stop') : t('pages.extras.actions.start')}>
      <Button
        size="large"
        icon={<PoweroffOutlined />}
        loading={busy === 'start' || busy === 'stop'}
        onClick={() => doAction(runningNow ? 'stop' : 'start')}
        aria-label={runningNow ? t('pages.extras.actions.stop') : t('pages.extras.actions.start')}
      >
        {runningNow ? t('pages.extras.actions.stop') : t('pages.extras.actions.start')}
      </Button>
    </Tooltip>,
    <Tooltip key="restart" title={t('pages.extras.actions.restart')}>
      <Button size="large" icon={<RedoOutlined />} loading={busy === 'restart'} onClick={() => doAction('restart')} aria-label="restart">
        {t('pages.extras.actions.restart')}
      </Button>
    </Tooltip>,
    <Tooltip key="edit" title={t('pages.extras.actions.config')}>
      <Button size="large" icon={<SettingOutlined />} onClick={openEdit} aria-label="edit">
        {t('pages.extras.actions.config')}
      </Button>
    </Tooltip>,
    <Tooltip key="delete" title={t('pages.extras.actions.delete')}>
      <Button size="large" danger icon={<DeleteOutlined />} loading={busy === 'delete'} onClick={onDelete} aria-label="delete">
        {t('pages.extras.actions.delete')}
      </Button>
    </Tooltip>,
  ];

  return (
    <>
      <Card title={displayName} variant="borderless" className={cardClass} actions={actions}>
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Row gutter={[12, 8]}>
            <Col span={10}><Text type="secondary">{t('pages.extras.enabled')}</Text></Col>
            <Col span={14}><Tag color={service.enabled ? 'green' : 'default'}>{service.enabled ? t('pages.extras.yes') : t('pages.extras.no')}</Tag></Col>
            <Col span={10}><Text type="secondary">{t('pages.extras.running')}</Text></Col>
            <Col span={14}><Tag color={service.running ? 'green' : 'red'}>{service.running ? t('pages.extras.yes') : t('pages.extras.no')}</Tag></Col>
            <Col span={10}><Text type="secondary">{t('pages.extras.binary')}</Text></Col>
            <Col span={14}><Tag color={service.binaryExists ? 'green' : 'orange'}>{service.binaryExists ? t('pages.extras.present') : t('pages.extras.missing')}</Tag></Col>
          </Row>
          {name === 'olcrtc' && service?.connectUri && (
            <div>
              <Text type="secondary" style={{ display: 'block', marginBottom: 4 }}>{t('pages.extras.connectUri')}</Text>
              <Typography.Paragraph copyable={{ text: service.connectUri }} style={{ marginBottom: 0 }}>
                <Typography.Text style={{ wordBreak: 'break-all' }}>{service.connectUri}</Typography.Text>
              </Typography.Paragraph>
              <div style={{ display: 'flex', justifyContent: 'center' }}>
                <QRCode value={service.connectUri} size={296} style={{ marginTop: 8 }} color="#000000" bgColor="#ffffff" />
              </div>
            </div>
          )}
          <Upload accept="*" showUploadList={false} customRequest={onUpload}>
            <Button icon={<UploadOutlined />}>{t('pages.extras.upload')}</Button>
          </Upload>
          {name === 'qwdtt' && (
            <Button icon={<RobotOutlined />} onClick={openTg}>
              {t('pages.extras.tgButton')}
            </Button>
          )}
          {name === 'qwdtt' && (
            <div>
              <Space style={{ width: '100%', justifyContent: 'space-between', marginBottom: 8 }}>
                <Text strong>{t('pages.extras.clients')}</Text>
                <Button size="small" icon={<PlusOutlined />} onClick={() => openClient(null)}>
                  {t('pages.extras.clientAdd')}
                </Button>
              </Space>
              <Table<WDTTClient>
                rowKey={(_, i) => String(i)}
                size="small"
                dataSource={clients}
                pagination={false}
                locale={{ emptyText: t('pages.extras.clientNoClients') }}
                columns={[
                  {
                    title: t('pages.extras.clientName'),
                    dataIndex: 'name',
                    ellipsis: true,
                    width: 140,
                  },
                  {
                    title: t('pages.extras.clientSubscriptionName'),
                    dataIndex: 'subscriptionName',
                    ellipsis: true,
                    width: 140,
                  },
                  {
                    title: t('pages.extras.clientPassword'),
                    dataIndex: 'password',
                    width: 110,
                    render: (v: string, _: WDTTClient, i: number) => (
                      <Text
                        code
                        style={{ cursor: 'pointer', fontSize: 12, userSelect: 'none' }}
                        onClick={() => {
                          setVisiblePasswords(prev => {
                            const next = new Set(prev);
                            next.has(i) ? next.delete(i) : next.add(i);
                            return next;
                          });
                        }}
                      >
                        {visiblePasswords.has(i) ? v : '\u2022'.repeat(8)}
                      </Text>
                    ),
                  },
                  {
                    title: t('pages.extras.enabled'),
                    dataIndex: 'enabled',
                    width: 70,
                    render: (v: boolean, _: WDTTClient, i: number) => (
                      <Switch size="small" checked={v} onChange={(checked) => onClientToggle(i, checked)} />
                    ),
                  },
                  {
                    title: t('pages.extras.actions.label'),
                    width: 128,
                    render: (_: WDTTClient, cl: WDTTClient, i: number) => (
                      <Space size={0}>
                        <Button size="small" type="text" icon={<InfoCircleOutlined />} onClick={() => setInfoClient(cl)} />
                        <Button size="small" type="text" icon={<QrcodeOutlined />} onClick={() => setQrClient({ client: cl, index: i })} />
                        <Button size="small" type="text" icon={<EditOutlined />} onClick={() => openClient(i)} />
                        <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => onClientDelete(i)} />
                      </Space>
                    ),
                  },
                ]}
              />
            </div>
          )}
        </Space>
      </Card>

      <LazyMount when={editOpen}>
        <Modal open={editOpen} title={t('pages.extras.editTitle')} onOk={onSave} onCancel={() => setEditOpen(false)} okText={t('save')} cancelText={t('cancel')}>
          <Form form={form} layout="vertical">
            <Form.Item name="enabled" label={t('pages.extras.enabled')} valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="autoStart" label={t('pages.extras.autoStart')} valuePropName="checked"><Switch /></Form.Item>
            {name === 'qwdtt' && (
              <>
                <Form.Item name="listenAddr" label={t('pages.extras.listenAddr')}><Input placeholder="0.0.0.0:56000" /></Form.Item>
                <Form.Item name="wgPort" label={t('pages.extras.wgPort')}><InputNumber min={1} max={65535} style={{ width: '100%' }} /></Form.Item>
                <Form.Item name="dns" label={t('pages.extras.dns')}><Input placeholder="8.8.8.8" /></Form.Item>
                <Form.Item name="listenRaw" label={t('pages.extras.listenRaw')} tooltip={t('pages.extras.listenRawDesc')}>
                  <Input placeholder="0.0.0.0:56003" />
                </Form.Item>
                <Form.Item name="configDir" label={t('pages.extras.configDir')}><Input placeholder="/etc/wdtt" /></Form.Item>
                <Form.Item name="subToken" label={t('pages.extras.subToken')}>
                  <Input placeholder="secret-token" style={{ fontFamily: 'monospace' }} />
                </Form.Item>
              </>
            )}
            {name === 'olcrtc' && (
              <>
                <Form.Item name="provider" label={t('pages.extras.olcrtcProvider')}>
                  <Select
                    options={[
                      { value: 'jitsi', label: 'Jitsi' },
                      { value: 'telemost', label: 'Telemost' },
                      { value: 'wbstream', label: 'WB Stream' },
                    ]}
                  />
                </Form.Item>
                <Form.Item name="roomId" label={t('pages.extras.olcrtcRoom')} rules={[{ required: true }]}>
                  <Input placeholder="https://meet.example.org/room" />
                </Form.Item>
                <Form.Item name="cryptoKey" label={t('pages.extras.olcrtcKey')}>
                  <Input.Password
                    placeholder="64 hex chars"
                    style={{ fontFamily: 'monospace' }}
                    addonAfter={<Button onClick={generateKey}>{t('pages.extras.olcrtcKeyGenerate')}</Button>}
                  />
                </Form.Item>
                <Form.Item name="transport" label={t('pages.extras.olcrtcTransport')}>
                  <Select
                    options={[
                      { value: 'datachannel', label: 'DataChannel' },
                      { value: 'vp8channel', label: 'VP8' },
                    ]}
                    disabled={provider === 'telemost'}
                  />
                </Form.Item>
                <Form.Item name="olcrtcDns" label={t('pages.extras.olcrtcDns')}><Input placeholder="8.8.8.8:53" /></Form.Item>
                {transport === 'vp8channel' && (
                  <>
                    <Form.Item name="vp8Fps" label={t('pages.extras.olcrtcVp8Fps')} tooltip={t('pages.extras.olcrtcVp8FpsDesc')}>
                      <InputNumber min={1} max={120} style={{ width: '100%' }} />
                    </Form.Item>
                    <Form.Item name="vp8Batch" label={t('pages.extras.olcrtcVp8Batch')} tooltip={t('pages.extras.olcrtcVp8BatchDesc')}>
                      <InputNumber min={1} max={64} style={{ width: '100%' }} />
                    </Form.Item>
                  </>
                )}
                <Form.Item name="debug" label={t('pages.extras.olcrtcDebug')} valuePropName="checked"><Switch /></Form.Item>
                <Form.Item name="configFile" label={t('pages.extras.configFile')}><Input placeholder="/etc/olcrtc/server.yaml" /></Form.Item>
                <Form.Item name="dataDir" label={t('pages.extras.dataDir')}><Input placeholder="/etc/olcrtc/data" /></Form.Item>
              </>
            )}
          </Form>
        </Modal>
      </LazyMount>

      <LazyMount when={clientOpen}>
        <Modal
          open={clientOpen}
          title={clientIndex !== null ? t('pages.extras.clientEdit') : t('pages.extras.clientAdd')}
          onOk={() => void onClientSave()}
          onCancel={() => setClientOpen(false)}
          okText={t('save')}
          cancelText={t('cancel')}
        >
          <Form form={clientForm} layout="vertical">
            <Form.Item name="name" label={t('pages.extras.clientName')} rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="subscriptionName" label={t('pages.extras.clientSubscriptionName')} rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="subscriptionDescription" label={t('pages.extras.clientSubscriptionDescription')}>
              <Input.TextArea rows={2} />
            </Form.Item>
            <Form.Item name="password" label={t('pages.extras.clientPassword')} rules={[{ required: true }]}>
              <Input
                style={{ fontFamily: 'monospace' }}
                addonAfter={<Button size="small" onClick={generateClientPassword}>{t('pages.extras.clientPasswordGenerate')}</Button>}
              />
            </Form.Item>
            <Form.Item name="vkHashes" label={t('pages.extras.clientVkHashes')}>
              <Input.TextArea rows={2} placeholder="hash1,hash2" />
            </Form.Item>
            <Form.Item name="enabled" label={t('pages.extras.clientEnable')} valuePropName="checked" initialValue={true}>
              <Switch />
            </Form.Item>
          </Form>
        </Modal>
      </LazyMount>

      <LazyMount when={infoClient !== null}>
        <Modal
          open={infoClient !== null}
          title={`${t('pages.extras.clientInfo')} — ${infoClient?.name ?? ''}`}
          footer={null}
          onCancel={() => setInfoClient(null)}
        >
          <Space direction="vertical" style={{ width: '100%' }}>
            <Text strong>{t('pages.extras.clientName')}:</Text>
            <Text>{infoClient?.name}</Text>
            <Text strong>{t('pages.extras.clientSubscriptionName')}:</Text>
            <Text>{infoClient?.subscriptionName}</Text>
            <Text strong>{t('pages.extras.clientSubscriptionDescription')}:</Text>
            <Text>{infoClient?.subscriptionDescription || '—'}</Text>
            <Text strong>{t('pages.extras.clientVkHashes')}:</Text>
            <Text>{infoClient?.vkHashes || '—'}</Text>
          </Space>
        </Modal>
      </LazyMount>

      <LazyMount when={qrClient !== null}>
        <Modal
          open={qrClient !== null}
          title={`${t('pages.extras.clientQr')} — ${qrClient?.client?.name ?? ''}`}
          footer={null}
          onCancel={() => setQrClient(null)}
          width={420}
        >
          <Collapse
            defaultActiveKey={['sub-info']}
            items={[
              {
                key: 'sub-info',
                label: t('pages.extras.clientSubscriptionInfo'),
                children: (
                  <div style={{ textAlign: 'center' }}>
                    <Text style={{ display: 'block', marginBottom: 8 }}>
                      {qrClient?.client?.name} — {t('pages.extras.clientSubscriptionInfo')}
                    </Text>
                    {qrClient?.client?.subscriptionDescription && (
                      <Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
                        {qrClient.client.subscriptionDescription}
                      </Text>
                    )}
                    {qrClient && subLink && qrClient.client.subUri ? (
                      <>
                        <Text copyable={{ text: subLink(qrClient.client.subUri) }} style={{ display: 'block', marginBottom: 8, wordBreak: 'break-all' }}>
                          {subLink(qrClient.client.subUri)}
                        </Text>
                        <Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
                          {t('pages.extras.subInfoQrHint')}
                        </Text>
                        <div style={{ display: 'flex', justifyContent: 'center' }}>
                          {qrClient && subDoc(qrClient.client) ? (
                            <QRCode value={subDoc(qrClient.client) as string} size={240} bordered={false} color="#000000" bgColor="#ffffff" />
                          ) : (
                            <Text type="secondary">—</Text>
                          )}
                        </div>
                      </>
                    ) : (
                      <Text type="secondary">—</Text>
                    )}
                  </div>
                ),
              },
              {
                key: 'qwdtt-link',
                label: t('pages.extras.clientQwdttLink'),
                children: (
                  <div style={{ textAlign: 'center' }}>
                    {qrClient && wdttLink(qrClient.client) && (
                      <>
                        <Text style={{ display: 'block', marginBottom: 8 }}>
                          {qrClient.client.name} — {t('pages.extras.clientQwdttLink')}{' '}
                          <Button
                            size="small"
                            type="text"
                            icon={<CopyOutlined />}
                            onClick={() => {
                              const link = wdttLink(qrClient.client);
                              if (link) {
                                navigator.clipboard.writeText(link).catch(() => {});
                                message.success(t('copySuccess'));
                              }
                            }}
                          />
                        </Text>
                        <div style={{ display: 'flex', justifyContent: 'center' }}>
                          <QRCode value={wdttLink(qrClient.client) ?? ''} size={240} bordered={false} color="#000000" bgColor="#ffffff" />
                        </div>
                      </>
                    )}
                    {qrClient && !wdttLink(qrClient.client) && (
                      <Text type="secondary">—</Text>
                    )}
                  </div>
                ),
              },
            ]}
          />
        </Modal>
      </LazyMount>

      <LazyMount when={tgOpen}>
        <Modal open={tgOpen} title={t('pages.extras.tgButton')} onOk={() => void onTgSave()} onCancel={() => setTgOpen(false)} okText={t('save')} cancelText={t('cancel')}>
          <Form form={tgForm} layout="vertical">
            <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>{t('pages.extras.tgDesc')}</Text>
            <Form.Item name="adminId" label={t('pages.extras.tgAdminId')}>
              <Input placeholder="123456789" />
            </Form.Item>
            <Form.Item name="botToken" label={t('pages.extras.tgBotToken')}>
              <Input.Password placeholder="1234567890:AA...-token" style={{ fontFamily: 'monospace' }} />
            </Form.Item>
          </Form>
        </Modal>
      </LazyMount>
    </>
  );
}

export default function ExtrasPage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => { setMessageInstance(messageApi); }, [messageApi]);
  usePageTitle();

  const pageClass = useMemo(() => {
    const classes = ['extras-page'];
    if (isDark) classes.push('is-dark');
    if (isUltra) classes.push('is-ultra');
    return classes.join(' ');
  }, [isDark, isUltra]);

  return (
    <ConfigProvider theme={antdThemeConfig}>
      <App>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin spinning={false}>
              <Title level={3}>{t('menu.extras')}</Title>
              <Text type="secondary" style={{ display: 'block', marginBottom: 24 }}>{t('pages.extras.desc')}</Text>
              <Row gutter={[16, 16]}>
                <Col xs={24} lg={12}><ServiceCard name="qwdtt" displayName="qWDTT" isDark={isDark} isUltra={isUltra} /></Col>
                <Col xs={24} lg={12}><ServiceCard name="olcrtc" displayName="olcRTC" isDark={isDark} isUltra={isUltra} /></Col>
              </Row>
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
      </App>
    </ConfigProvider>
  );
}
