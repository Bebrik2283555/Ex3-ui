import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
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
  Result,
  Row,
  Segmented,
  Select,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
  Typography,
  Upload,
  message,
} from 'antd';
import type { UploadProps } from 'antd';
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  DownloadOutlined,
  FileTextOutlined,
  PlayCircleOutlined,
  PoweroffOutlined,
  RedoOutlined,
  SaveOutlined,
  SettingOutlined,
  SyncOutlined,
  UploadOutlined,
} from '@ant-design/icons';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import {
  useZapretStatus,
  useZapretHosts,
  useZapretMutations,
  useZapretFiles,
  useWarpStatus,
  useWarpSettings,
  useWarpMutations,
  useWarpInstallLogs,
  useWarpLogs,
  type WarpSettings,
} from '@/api/queries/useSystemTools';

const { Text, Title } = Typography;

const ZAPRET_DOWNLOAD_URL =
  'https://github.com/ImMALWARE/zapret-linux-easy/archive/refs/heads/main.zip';

// ─── Zapret tab ──────────────────────────────────────────────────────────────

function ZapretTab() {
  const { t } = useTranslation();

  const { data: status, isLoading, isError, refetch } = useZapretStatus();
  const { data: hosts, refetch: refetchHosts } = useZapretHosts();
  const { data: files, refetch: refetchFiles } = useZapretFiles();
  const {
    downloadInstall,
    uninstall,
    start,
    stop,
    restart,
    saveHosts,
    saveConfig,
    saveListFile,
    restoreZip,
  } = useZapretMutations();
  const [dlForm] = Form.useForm<{ firewall: string; ifaceWan: string; ifaceLan: string }>();
  const [bypassText, setBypassText] = useState('');
  const [ignoreText, setIgnoreText] = useState('');
  const [busy, setBusy] = useState<string | null>(null);
  const [configOpen, setConfigOpen] = useState(false);
  const [configText, setConfigText] = useState('');
  const [listsOpen, setListsOpen] = useState(false);
  const [listName, setListName] = useState('');
  const [listText, setListText] = useState('');
  const [backupOpen, setBackupOpen] = useState(false);
  const [hostsSynced, setHostsSynced] = useState<typeof hosts>(undefined);
  if (hosts && hostsSynced !== hosts) {
    setHostsSynced(hosts);
    setBypassText(hosts.bypass.join('\n'));
    setIgnoreText(hosts.ignore.join('\n'));
  }

  const editableLists = useMemo(() => {
    if (!files) return [];
    return Object.keys(files).filter(
      (name) => name !== 'config.txt' && name !== 'autohosts.txt' && name !== 'ignore.txt',
    );
  }, [files]);

  const openConfig = async () => {
    setConfigOpen(true);
    const { data } = await refetchFiles();
    setConfigText(data?.['config.txt'] ?? '');
  };

  const openLists = async () => {
    setListsOpen(true);
    const { data } = await refetchFiles();
    const names = data
      ? Object.keys(data).filter(
          (n) => n !== 'config.txt' && n !== 'autohosts.txt' && n !== 'ignore.txt',
        )
      : [];
    setListName(names[0] ?? '');
    setListText(names[0] ? (data?.[names[0]] ?? '') : '');
  };

  const runSaveConfig = async () => {
    setBusy('config');
    try {
      await saveConfig(configText);
      setConfigOpen(false);
    } finally {
      setBusy(null);
      await refetch();
      await refetchHosts();
    }
  };

  const runSaveList = async () => {
    if (!listName) return;
    setBusy('list');
    try {
      await saveListFile(listName, listText);
      setListsOpen(false);
    } finally {
      setBusy(null);
      await refetch();
      await refetchHosts();
    }
  };

  const runBackup = () => {
    window.location.href = (window.X_UI_BASE_PATH || '') + 'panel/api/zapret/backup';
  };

  const onRestore: UploadProps['customRequest'] = async (options) => {
    const file = options.file as File;
    try {
      await restoreZip(file);
      setBackupOpen(false);
    } catch {
      // message about the failed restore is shown by the mutation layer
    } finally {
      options.onSuccess?.(null);
    }
  };

  const run = async (kind: 'uninstall' | 'start' | 'stop' | 'restart') => {
    setBusy(kind);
    try {
      if (kind === 'uninstall') await uninstall();
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
      const cfg = {
        url: ZAPRET_DOWNLOAD_URL,
        firewall: values.firewall || 'nftables',
        ifaceWan: values.ifaceWan ?? '',
        ifaceLan: values.ifaceLan ?? '',
      };
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
        bypass: bypassText
          .split('\n')
          .map((s) => s.trim())
          .filter(Boolean),
        ignore: ignoreText
          .split('\n')
          .map((s) => s.trim())
          .filter(Boolean),
      });
    } finally {
      setBusy(null);
      await refetchHosts();
    }
  };

  const tag = (label: React.ReactNode, ok: boolean) => (
    <Tag color={ok ? 'green' : 'red'} icon={ok ? <CheckCircleOutlined /> : <CloseCircleOutlined />}>
      {label}
    </Tag>
  );

  return (
    <Spin spinning={isLoading}>
      <Card title={t('pages.zapret.statusCard')} variant="borderless" style={{ marginBottom: 16 }}>
        {isError ? (
          <Result status="error" title={t('pages.zapret.installFailed')} />
        ) : !status ? null : (
          <Space wrap size="large">
            {tag(t('pages.zapret.installed'), status.installed)}
            {status.installed && tag(t('pages.zapret.running'), status.running)}
            {status.firewall && (
              <Text>
                {t('pages.zapret.firewall')}: <Text strong>{status.firewall}</Text>
              </Text>
            )}
          </Space>
        )}
      </Card>

      {!status?.installed ? (
        <Card
          title={t('pages.zapret.downloadTitle')}
          variant="borderless"
          style={{ marginBottom: 16 }}
        >
          <Form form={dlForm} layout="vertical" initialValues={{ firewall: 'nftables' }}>
            <Form.Item label={t('pages.zapret.downloadUrl')}>
              <Input
                value={ZAPRET_DOWNLOAD_URL}
                readOnly
                placeholder={t('pages.zapret.downloadPlaceholder')}
                style={{ fontFamily: 'monospace' }}
              />
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
            <Button
              type="primary"
              icon={<DownloadOutlined />}
              loading={busy === 'download'}
              onClick={() => void runDownload()}
            >
              {t('pages.zapret.downloadBtn')}
            </Button>
          </Form>
        </Card>
      ) : (
        <>
          <Card
            title={t('pages.zapret.hostsTitle')}
            variant="borderless"
            style={{ marginBottom: 16 }}
          >
            <Space wrap style={{ marginBottom: 16 }}>
              <Button
                icon={<RedoOutlined />}
                loading={busy === 'restart'}
                onClick={() => void run('restart')}
              >
                {t('pages.zapret.restartBtn')}
              </Button>
              <Button
                danger
                icon={<PoweroffOutlined />}
                loading={busy === 'stop'}
                onClick={() => void run('stop')}
              >
                {t('pages.zapret.stopBtn')}
              </Button>
              <Button
                icon={<PlayCircleOutlined />}
                loading={busy === 'start'}
                onClick={() => void run('start')}
              >
                {t('pages.zapret.startBtn')}
              </Button>
            </Space>
            <Row gutter={16}>
              <Col xs={24} md={12}>
                <Text strong>{t('pages.zapret.bypassLabel')}</Text>
                <Input.TextArea
                  rows={10}
                  value={bypassText}
                  onChange={(e) => setBypassText(e.target.value)}
                  placeholder="example.com"
                />
              </Col>
              <Col xs={24} md={12}>
                <Text strong>{t('pages.zapret.ignoreLabel')}</Text>
                <Input.TextArea
                  rows={10}
                  value={ignoreText}
                  onChange={(e) => setIgnoreText(e.target.value)}
                />
              </Col>
            </Row>
            <Space wrap style={{ marginTop: 16 }}>
              <Button
                type="primary"
                icon={<SaveOutlined />}
                loading={busy === 'hosts'}
                onClick={() => void runSaveHosts()}
              >
                {t('pages.zapret.saveHosts')}
              </Button>
              <Button
                icon={<SettingOutlined />}
                loading={busy === 'config'}
                onClick={() => void openConfig()}
              >
                {t('pages.zapret.strategyBtn')}
              </Button>
              <Button
                icon={<FileTextOutlined />}
                loading={busy === 'list'}
                onClick={() => void openLists()}
              >
                {t('pages.zapret.listBtn')}
              </Button>
              <Button icon={<DownloadOutlined />} onClick={() => setBackupOpen(true)}>
                {t('pages.zapret.backupBtn')}
              </Button>
            </Space>
            <Modal
              open={configOpen}
              title={t('pages.zapret.strategyTitle')}
              onOk={() => void runSaveConfig()}
              onCancel={() => setConfigOpen(false)}
              okText={t('save')}
              cancelText={t('cancel')}
              confirmLoading={busy === 'config'}
            >
              <Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
                {t('pages.zapret.strategyHint')}
              </Text>
              <Input.TextArea
                rows={16}
                value={configText}
                onChange={(e) => setConfigText(e.target.value)}
                style={{ fontFamily: 'monospace' }}
              />
            </Modal>
            <Modal
              open={listsOpen}
              title={t('pages.zapret.listTitle')}
              onOk={() => void runSaveList()}
              onCancel={() => setListsOpen(false)}
              okText={t('save')}
              cancelText={t('cancel')}
              confirmLoading={busy === 'list'}
            >
              <Select
                style={{ width: '100%', marginBottom: 8 }}
                value={listName}
                placeholder={t('pages.zapret.listSelect')}
                options={editableLists.map((name) => ({ value: name, label: name }))}
                onChange={(name) => {
                  setListName(name);
                  setListText(files?.[name] ?? '');
                }}
              />
              <Input.TextArea
                rows={14}
                value={listText}
                onChange={(e) => setListText(e.target.value)}
                style={{ fontFamily: 'monospace' }}
              />
            </Modal>
          </Card>
          <Card title={t('pages.zapret.uninstallBtn')} variant="borderless">
            <Button danger loading={busy === 'uninstall'} onClick={() => void run('uninstall')}>
              {t('pages.zapret.uninstallBtn')}
            </Button>
          </Card>
          <Modal
            open={backupOpen}
            title={t('pages.zapret.backupBtn')}
            footer={null}
            onCancel={() => setBackupOpen(false)}
          >
            <Space direction="vertical" style={{ width: '100%' }}>
              <Button block icon={<DownloadOutlined />} onClick={runBackup}>
                {t('pages.zapret.backupDownload')}
              </Button>
              <Upload
                accept=".zip,application/zip"
                showUploadList={false}
                customRequest={onRestore}
              >
                <Button block icon={<UploadOutlined />}>
                  {t('pages.zapret.restoreBtn')}
                </Button>
              </Upload>
              <Text type="secondary">{t('pages.zapret.restoreHint')}</Text>
            </Space>
          </Modal>
        </>
      )}
    </Spin>
  );
}

// ─── Warp tab ─────────────────────────────────────────────────────────────────

function WarpTab() {
  const { t } = useTranslation();
  const [busy, setBusy] = useState<string | null>(null);
  const [logsOpen, setLogsOpen] = useState(false);
  const [logTab, setLogTab] = useState<'watch' | 'install'>('watch');
  const [settingsModalOpen, setSettingsModalOpen] = useState(false);
  const [isInstallMode, setIsInstallMode] = useState(false);
  const [settingsForm] = Form.useForm<{
    timeout?: number;
    checkInterval?: number;
    checkUrl?: string;
    autoRotate?: boolean;
  }>();

  // Start with a 5s baseline poll; once we know the state we refetch faster if needed
  const { data: status, refetch: refetchStatus } = useWarpStatus(5000);
  const isWorking = status?.installing || (status?.installed && status?.rotating);
  // Derive a fast refetch interval while any long task is running
  useEffect(() => {
    if (!isWorking) return;
    const id = setInterval(() => {
      void refetchStatus();
    }, 2000);
    return () => clearInterval(id);
  }, [isWorking, refetchStatus]);

  const [prevInstalling, setPrevInstalling] = useState(false);
  if (status?.installing && !prevInstalling) {
    setPrevInstalling(true);
    setLogTab('install');
    setLogsOpen(true);
  } else if (!status?.installing && prevInstalling) {
    setPrevInstalling(false);
    setLogTab('install');
    setLogsOpen(true);
  }

  const { data: installLogs, refetch: refetchInstallLogs } = useWarpInstallLogs(
    status?.installing ? 2000 : logsOpen && logTab === 'install' ? 5000 : undefined,
  );
  const { data: watchLogs, refetch: refetchWatchLogs } = useWarpLogs(
    logsOpen && logTab === 'watch',
  );
  const { data: savedSettings, refetch: refetchSettings } = useWarpSettings(status?.installed);
  const { install, uninstall, start, stop, restart, rotate, saveSettings } = useWarpMutations();

  const handleOpenInstall = () => {
    setIsInstallMode(true);
    settingsForm.resetFields();
    settingsForm.setFieldsValue({
      timeout: undefined,
      checkInterval: undefined,
      checkUrl: '',
      autoRotate: true,
    });
    setSettingsModalOpen(true);
  };

  const handleOpenSettings = async () => {
    setIsInstallMode(false);
    settingsForm.resetFields();
    const res = await refetchSettings();
    const current = res.data ?? savedSettings;
    if (current) {
      settingsForm.setFieldsValue({
        timeout: current.timeout === 5 ? undefined : current.timeout,
        checkInterval: current.checkInterval === 5 ? undefined : current.checkInterval,
        checkUrl: current.checkUrl === 'https://api.telegram.org/' ? '' : current.checkUrl,
        autoRotate: current.autoRotate ?? true,
      });
    } else {
      settingsForm.setFieldsValue({
        timeout: undefined,
        checkInterval: undefined,
        checkUrl: '',
        autoRotate: true,
      });
    }
    setSettingsModalOpen(true);
  };

  useEffect(() => {
    if (!settingsModalOpen) return;
    if (isInstallMode) {
      settingsForm.setFieldsValue({
        timeout: undefined,
        checkInterval: undefined,
        checkUrl: '',
        autoRotate: true,
      });
    } else if (savedSettings) {
      settingsForm.setFieldsValue({
        timeout: savedSettings.timeout === 5 ? undefined : savedSettings.timeout,
        checkInterval: savedSettings.checkInterval === 5 ? undefined : savedSettings.checkInterval,
        checkUrl:
          savedSettings.checkUrl === 'https://api.telegram.org/' ? '' : savedSettings.checkUrl,
        autoRotate: savedSettings.autoRotate ?? true,
      });
    }
  }, [settingsModalOpen, isInstallMode, savedSettings, settingsForm]);

  const handleSaveSettings = async () => {
    try {
      const values = await settingsForm.validateFields();
      const payload: WarpSettings = {
        domains: [],
        timeout: values.timeout ? Number(values.timeout) : 5,
        checkInterval: values.checkInterval ? Number(values.checkInterval) : 5,
        checkUrl: values.checkUrl?.trim() || 'https://api.telegram.org/',
        autoRotate: values.autoRotate ?? true,
      };

      setBusy(isInstallMode ? 'install' : 'settings');
      try {
        if (isInstallMode) {
          await install(payload);
          setSettingsModalOpen(false);
        } else {
          await saveSettings(payload);
          setSettingsModalOpen(false);
        }
      } finally {
        setBusy(null);
        await refetchStatus();
        if (!isInstallMode) {
          await refetchSettings();
        }
      }
    } catch {
      // validation error
    }
  };

  const run = useCallback(
    async (kind: 'uninstall' | 'start' | 'stop' | 'restart' | 'rotate') => {
      setBusy(kind);
      try {
        if (kind === 'uninstall') {
          await uninstall();
          setLogsOpen(false);
          setLogTab('watch');
        } else if (kind === 'start') await start();
        else if (kind === 'stop') await stop();
        else if (kind === 'restart') await restart();
        else await rotate();
      } finally {
        setBusy(null);
        await refetchStatus();
      }
    },
    [uninstall, start, stop, restart, rotate, refetchStatus],
  );

  const tag = (label: React.ReactNode, ok: boolean) => (
    <Tag color={ok ? 'green' : 'red'} icon={ok ? <CheckCircleOutlined /> : <CloseCircleOutlined />}>
      {label}
    </Tag>
  );

  return (
    <>
      {/* Status card */}
      <Card title={t('pages.warp.statusCard')} variant="borderless" style={{ marginBottom: 16 }}>
        <Space wrap size="large">
          {tag(t('pages.warp.installed'), !!status?.installed)}
          {status?.installed && tag(t('pages.warp.running'), !!status.running)}
          {status?.installed && (
            <Tag
              color={status.watchdogActive ? 'green' : 'orange'}
              icon={status.watchdogActive ? <CheckCircleOutlined /> : <CloseCircleOutlined />}
            >
              {t('pages.warp.watchdog')}
            </Tag>
          )}
          {status?.installed && (
            <Tag color="blue">
              WARP: {status.ip || '—'}
              {status.country ? ` (${status.country})` : ''}
            </Tag>
          )}
          {isWorking && (
            <Tag icon={<SyncOutlined spin />} color="processing">
              {status?.installing ? t('pages.warp.installing') : t('pages.warp.rotating')}
            </Tag>
          )}
        </Space>
      </Card>

      {/* Unified log panel: install log while installing, watchdog log otherwise */}
      {(status?.installing || (installLogs && installLogs.length > 0) || status?.installed) && (
        <Card variant="borderless" style={{ marginBottom: 16 }}>
          <Collapse
            activeKey={logsOpen ? ['log'] : []}
            onChange={(keys) => {
              const open = Array.isArray(keys) ? keys.includes('log') : keys === 'log';
              setLogsOpen(open);
              if (open) {
                if (logTab === 'watch') void refetchWatchLogs();
                else void refetchInstallLogs();
              }
            }}
            items={[
              {
                key: 'log',
                label: (
                  <Space onClick={(e) => e.stopPropagation()}>
                    <span>
                      {logTab === 'watch'
                        ? t('pages.warp.watchLogsTitle')
                        : t('pages.warp.installLogsTitle')}
                    </span>
                    <Segmented
                      size="small"
                      value={logTab}
                      onChange={(val) => {
                        const tab = val as 'watch' | 'install';
                        setLogTab(tab);
                        setLogsOpen(true);
                        if (tab === 'watch') void refetchWatchLogs();
                        else void refetchInstallLogs();
                      }}
                      options={[
                        { label: t('pages.warp.watchLogsTitle'), value: 'watch' },
                        { label: t('pages.warp.installLogsTitle'), value: 'install' },
                      ]}
                    />
                  </Space>
                ),
                extra: (
                  <Button
                    type="text"
                    size="small"
                    icon={<SyncOutlined />}
                    onClick={(e) => {
                      e.stopPropagation();
                      if (logTab === 'watch') void refetchWatchLogs();
                      else void refetchInstallLogs();
                    }}
                  />
                ),
                children: (
                  <pre
                    style={{
                      margin: 0,
                      maxHeight: 300,
                      overflow: 'auto',
                      fontSize: 12,
                      fontFamily: 'monospace',
                      whiteSpace: 'pre-wrap',
                      wordBreak: 'break-all',
                    }}
                  >
                    {logTab === 'install'
                      ? (installLogs ?? []).join('\n') || (status?.installing ? '…' : '—')
                      : (watchLogs ?? []).join('\n') || '—'}
                  </pre>
                ),
              },
            ]}
          />
        </Card>
      )}

      {/* Install card (not yet installed) */}
      {!status?.installed && !status?.installing && (
        <Card
          title={t('pages.warp.installTitle')}
          variant="borderless"
          style={{ marginBottom: 16 }}
        >
          <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
            {t('pages.warp.desc')}
          </Text>
          <Button
            type="primary"
            icon={<DownloadOutlined />}
            loading={busy === 'install'}
            onClick={handleOpenInstall}
          >
            {t('pages.warp.installBtn')}
          </Button>
        </Card>
      )}

      {/* Controls (installed) */}
      {status?.installed && (
        <>
          <Card
            title={t('pages.warp.controlsTitle')}
            variant="borderless"
            style={{ marginBottom: 16 }}
          >
            <Space wrap>
              <Button
                icon={<PlayCircleOutlined />}
                loading={busy === 'start'}
                onClick={() => void run('start')}
              >
                {t('pages.warp.startBtn')}
              </Button>
              <Button
                danger
                icon={<PoweroffOutlined />}
                loading={busy === 'stop'}
                onClick={() => void run('stop')}
              >
                {t('pages.warp.stopBtn')}
              </Button>
              <Button
                icon={<RedoOutlined />}
                loading={busy === 'restart'}
                onClick={() => void run('restart')}
              >
                {t('pages.warp.restartBtn')}
              </Button>
              <Button
                icon={<SettingOutlined />}
                loading={busy === 'settings'}
                onClick={() => void handleOpenSettings()}
              >
                {t('pages.warp.settingsBtn')}
              </Button>
              <Button
                icon={<SyncOutlined spin={status.rotating} />}
                loading={busy === 'rotate'}
                onClick={() => void run('rotate')}
                disabled={status.rotating}
              >
                {t('pages.warp.rotateBtn')}
              </Button>
            </Space>
          </Card>

          {/* Uninstall */}
          <Card title={t('pages.warp.uninstallBtn')} variant="borderless">
            <Button danger loading={busy === 'uninstall'} onClick={() => void run('uninstall')}>
              {t('pages.warp.uninstallBtn')}
            </Button>
          </Card>
        </>
      )}

      {/* Settings Modal */}
      <Modal
        title={isInstallMode ? t('pages.warp.installTitle') : t('pages.warp.settingsTitle')}
        open={settingsModalOpen}
        onCancel={() => setSettingsModalOpen(false)}
        onOk={() => void handleSaveSettings()}
        confirmLoading={busy === 'install' || busy === 'settings'}
        okText={isInstallMode ? t('pages.warp.installBtn') : t('pages.warp.settingsSave')}
      >
        <Form form={settingsForm} layout="vertical">
          <Alert
            type="info"
            showIcon
            message={t('pages.warp.domainsRoutingNote')}
            style={{ marginBottom: 16 }}
          />
          <Form.Item name="timeout" label={t('pages.warp.timeoutLabel')}>
            <InputNumber min={1} max={60} placeholder="5" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="checkInterval" label={t('pages.warp.checkIntervalLabel')}>
            <InputNumber min={1} max={1440} placeholder="5" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="checkUrl" label={t('pages.warp.checkUrlLabel')}>
            <Input placeholder="https://api.telegram.org/" />
          </Form.Item>
          <Form.Item
            name="autoRotate"
            label={t('pages.warp.autoRotateLabel')}
            extra={t('pages.warp.autoRotateHint')}
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

// ─── Page shell ──────────────────────────────────────────────────────────────

type TabKey = 'zapret' | 'warp';

export default function ZapretPage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);
  usePageTitle();
  const [activeTab, setActiveTab] = useState<TabKey>('zapret');

  const pageClass = useMemo(() => {
    const c = ['zapret-page'];
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
            <Title level={3}>{t('menu.zapret')}</Title>
            <Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
              {activeTab === 'zapret' ? t('pages.zapret.desc') : t('pages.warp.desc')}
            </Text>
            <Tabs
              activeKey={activeTab}
              onChange={(k) => setActiveTab(k as TabKey)}
              items={[
                { key: 'zapret', label: 'Zapret' },
                { key: 'warp', label: 'Warp' },
              ]}
              style={{ marginBottom: 16 }}
            />
            {activeTab === 'zapret' ? <ZapretTab /> : <WarpTab />}
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
