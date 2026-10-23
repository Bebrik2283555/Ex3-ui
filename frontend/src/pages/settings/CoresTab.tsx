import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Modal,
  Space,
  Spin,
  Tabs,
  Tag,
  message,
} from 'antd';
import {
  FileTextOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  StopOutlined,
} from '@ant-design/icons';

import { SizeFormatter } from '@/utils';
import {
  type CoreInfo,
  useCoreActions,
  useCoreDetailsQuery,
  useCoresQuery,
} from '@/api/queries/useCores';
import './CoresTab.css';

function stateTag(t: (k: string) => string, ok: boolean, yesKey: string, noKey: string) {
  return <Tag color={ok ? 'green' : 'default'}>{t(ok ? yesKey : noKey)}</Tag>;
}

function SidecarPanel({ core }: { core: CoreInfo }) {
  const { t } = useTranslation();
  const [messageApi, holder] = message.useMessage();
  const actions = useCoreActions();
  const [logsOpen, setLogsOpen] = useState(false);
  const details = useCoreDetailsQuery(core.name, logsOpen);
  const busy = actions.start.isPending || actions.stop.isPending;

  const run = async (fn: () => Promise<unknown>, okKey: string) => {
    try {
      await fn();
      messageApi.success(t(okKey));
    } catch (err) {
      messageApi.error(`${t('pages.cores.actionFailed')}: ${(err as Error).message}`);
    }
  };

  return (
    <>
      {holder}
      <Card
        title={core.displayName}
        extra={
          <Space wrap>
            <Button
              icon={core.running ? <StopOutlined /> : <PlayCircleOutlined />}
              disabled={!core.installed || core.inbounds === 0}
              loading={busy}
              onClick={() =>
                run(
                  () =>
                    core.running
                      ? actions.stop.mutateAsync(core.name)
                      : actions.start.mutateAsync(core.name),
                  core.running ? 'pages.cores.stopDone' : 'pages.cores.startDone',
                )
              }
            >
              {t(core.running ? 'pages.cores.disable' : 'pages.cores.enable')}
            </Button>
            <Button icon={<FileTextOutlined />} onClick={() => setLogsOpen(true)}>
              {t('pages.cores.logs')}
            </Button>
            <Button
              icon={<ReloadOutlined />}
              loading={actions.restart.isPending}
              disabled={!core.installed || core.inbounds === 0}
              onClick={() =>
                run(() => actions.restart.mutateAsync(core.name), 'pages.cores.restartDone')
              }
            >
              {t('pages.cores.restart')}
            </Button>
          </Space>
        }
      >
        {!core.installed && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 16 }}
            title={t('pages.cores.noBinary')}
          />
        )}
        {core.lastError && (
          <Alert type="error" showIcon style={{ marginBottom: 16 }} title={core.lastError} />
        )}
        <Descriptions
          rootClassName="cores-descriptions"
          column={1}
          size="small"
          bordered
          styles={{ label: { width: '40%' } }}
        >
          <Descriptions.Item label={t('pages.cores.installed')}>
            {stateTag(t, core.installed, 'pages.cores.installed', 'pages.cores.notInstalled')}
          </Descriptions.Item>
          <Descriptions.Item label={t('pages.cores.enabled')}>
            {stateTag(t, core.enabled, 'pages.cores.enabled', 'pages.cores.disabled')}
          </Descriptions.Item>
          <Descriptions.Item label={t('pages.cores.running')}>
            {stateTag(t, core.running, 'pages.cores.running', 'pages.cores.stopped')}
          </Descriptions.Item>
          <Descriptions.Item label={t('pages.cores.inbounds')}>{core.inbounds}</Descriptions.Item>
          <Descriptions.Item label={t('pages.cores.binaryPath')}>
            <code>{core.binary}</code>
          </Descriptions.Item>
          {core.installed && (
            <Descriptions.Item label={t('pages.cores.size')}>
              {SizeFormatter.sizeFormat(core.size)}
            </Descriptions.Item>
          )}
          {core.installed && (
            <Descriptions.Item label={t('pages.cores.updatedAt')}>
              {new Date(core.modified * 1000).toLocaleString()}
            </Descriptions.Item>
          )}
        </Descriptions>
      </Card>

      <Modal
        open={logsOpen}
        title={`${core.displayName} — ${t('pages.cores.logs')}`}
        footer={null}
        width={860}
        destroyOnHidden
        onCancel={() => setLogsOpen(false)}
      >
        {details.isLoading ? (
          <Spin />
        ) : (
          <Tabs
            items={(details.data?.inboundList ?? []).map((inbound) => ({
              key: String(inbound.id),
              label: inbound.remark || `#${inbound.id}`,
              children: (
                <pre style={{ margin: 0, maxHeight: 420, overflow: 'auto' }}>
                  {(inbound.logs ?? []).join('\n') || t('pages.cores.noLogs')}
                </pre>
              ),
            }))}
          />
        )}
      </Modal>
    </>
  );
}

export default function CoresTab() {
  const { t } = useTranslation();
  const cores = useCoresQuery();

  const items = useMemo(
    () =>
      (cores.data ?? []).map((core) => ({
        key: core.name,
        label: core.displayName,
        children: <SidecarPanel core={core} />,
      })),
    [cores.data],
  );

  if (cores.isLoading) return <Spin />;
  if (cores.isError) {
    return <Empty description={(cores.error as Error).message} />;
  }

  return (
    <>
      <Alert type="info" showIcon style={{ marginBottom: 16 }} title={t('pages.cores.desc')} />
      <Tabs items={items} destroyOnHidden />
    </>
  );
}
