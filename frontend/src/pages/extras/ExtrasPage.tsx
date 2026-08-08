import { useCallback, useEffect, useMemo, useState } from 'react';
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
  Select,
  Space,
  Spin,
  Switch,
  Tag,
  Tooltip,
  Typography,
  Upload,
  message,
} from 'antd';
import type { UploadProps } from 'antd';
import { DownloadOutlined, PlayCircleOutlined, PoweroffOutlined, RedoOutlined, SettingOutlined, UploadOutlined, DeleteOutlined } from '@ant-design/icons';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { LazyMount } from '@/components/utility';
import { useExtrasStatus, useExtrasMutations, type CoreName, type ExtraConfig } from '@/api/queries/useSystemTools';

const { Text, Title } = Typography;

function ServiceCard({ name, displayName, isDark, isUltra }: { name: CoreName; displayName: string; isDark: boolean; isUltra: boolean }) {
  const { t } = useTranslation();
  const { data: services, isLoading, isError, error } = useExtrasStatus();
  const { start, stop, restart, saveConfig, uploadBinary, downloadBinary, deleteBinary } = useExtrasMutations();
  const [editOpen, setEditOpen] = useState(false);
  const [dlOpen, setDlOpen] = useState(false);
  const [form] = Form.useForm<ExtraConfig>();
  const [dlForm] = Form.useForm<{ url: string }>();
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
    const base = window.X_UI_BASE_PATH?.replace(/\/+$/, '') ?? '';
    return `${window.location.origin}${base}/panel/qwdtt/sub/${encodeURIComponent(service.config.subToken)}`;
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
    Modal.confirm({
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
    await saveConfig(name, values);
    setEditOpen(false);
  };

  const onUpload: UploadProps['customRequest'] = async (options) => {
    const file = options.file as File;
    if (file) {
      await uploadBinary(name, file);
    }
    options.onSuccess?.(null);
  };

  const onDownload = async () => {
    const values = await dlForm.validateFields();
    setBusy('download');
    try {
      await downloadBinary(name, values.url);
    } finally {
      setBusy(null);
      setDlOpen(false);
      dlForm.resetFields();
    }
  };

  const generateKey = () => {
    const bytes = new Uint8Array(32);
    crypto.getRandomValues(bytes);
    form.setFieldValue('cryptoKey', Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join(''));
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
  const actions = [
    <Tooltip key="start" title={t('pages.extras.actions.start')}>
      <Button type="text" icon={<PlayCircleOutlined />} loading={busy === 'start'} onClick={() => doAction('start')} aria-label="start" />
    </Tooltip>,
    <Tooltip key="stop" title={t('pages.extras.actions.stop')}>
      <Button type="text" icon={<PoweroffOutlined />} loading={busy === 'stop'} onClick={() => doAction('stop')} aria-label="stop" />
    </Tooltip>,
    <Tooltip key="restart" title={t('pages.extras.actions.restart')}>
      <Button type="text" icon={<RedoOutlined />} loading={busy === 'restart'} onClick={() => doAction('restart')} aria-label="restart" />
    </Tooltip>,
    <Tooltip key="edit" title={t('pages.extras.actions.edit')}>
      <Button type="text" icon={<SettingOutlined />} onClick={openEdit} aria-label="edit" />
    </Tooltip>,
    <Tooltip key="delete" title={t('pages.extras.actions.delete')}>
      <Button type="text" danger icon={<DeleteOutlined />} loading={busy === 'delete'} onClick={onDelete} aria-label="delete" />
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
          {subLink && (
            <div>
              <Text type="secondary" style={{ display: 'block', marginBottom: 4 }}>{t('pages.extras.subscription')}</Text>
              <Typography.Paragraph copyable={{ text: subLink }} style={{ marginBottom: 0 }}>
                <Typography.Link href={subLink} target="_blank" rel="noopener noreferrer" ellipsis>
                  {subLink}
                </Typography.Link>
              </Typography.Paragraph>
            </div>
          )}
          {name === 'olcrtc' && service?.connectUri && (
            <div>
              <Text type="secondary" style={{ display: 'block', marginBottom: 4 }}>{t('pages.extras.connectUri')}</Text>
              <Typography.Paragraph copyable={{ text: service.connectUri }} style={{ marginBottom: 0 }}>
                <Typography.Text style={{ wordBreak: 'break-all' }}>{service.connectUri}</Typography.Text>
              </Typography.Paragraph>
            </div>
          )}
          <Upload accept="*" showUploadList={false} customRequest={onUpload}>
            <Button icon={<UploadOutlined />}>{t('pages.extras.upload')}</Button>
          </Upload>
          <Button icon={<DownloadOutlined />} onClick={() => setDlOpen(true)}>{t('pages.extras.downloadBtn')}</Button>
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
                <Form.Item name="password" label={t('pages.extras.password')}><Input.Password /></Form.Item>
                <Form.Item name="dns" label={t('pages.extras.dns')}><Input placeholder="8.8.8.8" /></Form.Item>
                <Form.Item name="listenRaw" label={t('pages.extras.listenRaw')} tooltip={t('pages.extras.listenRawDesc')}>
                  <Input placeholder="0.0.0.0:56003" />
                </Form.Item>
                <Form.Item name="configDir" label={t('pages.extras.configDir')}><Input placeholder="/etc/wdtt" /></Form.Item>
                <Form.Item name="subToken" label={t('pages.extras.subToken')}>
                  <Input placeholder="secret-token" style={{ fontFamily: 'monospace' }} />
                </Form.Item>
                <Form.Item name="subHost" label={t('pages.extras.subHost')}>
                  <Input placeholder="203.0.113.10:56000" />
                </Form.Item>
                <Form.Item name="vkHashes" label={t('pages.extras.vkHashes')}>
                  <Input.TextArea rows={2} placeholder="hash1,hash2" />
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
            {name === 'qwdtt' && (
              <Form.Item name="extraArgs" label={t('pages.extras.extraArgs')}><Input placeholder="-admin 123 -bot-token abc" /></Form.Item>
            )}
          </Form>
        </Modal>
      </LazyMount>

      <Modal
        open={dlOpen}
        title={t('pages.extras.downloadUrl')}
        onOk={() => void onDownload()}
        onCancel={() => setDlOpen(false)}
        okText={t('pages.extras.downloadBtn')}
        cancelText={t('cancel')}
        confirmLoading={busy === 'download'}
      >
        <Form form={dlForm} layout="vertical" initialValues={{ url: '' }}>
          <Form.Item name="url" label={t('pages.extras.downloadUrl')} rules={[{ required: true }]}>
            <Input placeholder={t('pages.extras.downloadPlaceholder')} style={{ fontFamily: 'monospace' }} />
          </Form.Item>
        </Form>
      </Modal>
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
    </ConfigProvider>
  );
}