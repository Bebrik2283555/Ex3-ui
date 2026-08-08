import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Card, ConfigProvider, Input, Layout, Result, Space, Spin, Table, Typography, message } from 'antd';
import type { TableColumnsType } from 'antd';
import { DownloadOutlined, SaveOutlined } from '@ant-design/icons';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { useHostsFile, useHostsFileMutations } from '@/api/queries/useSystemTools';

const { Text, Title } = Typography;

export default function HostsFilePage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => { setMessageInstance(messageApi); }, [messageApi]);
  usePageTitle();

  const { data, isLoading, isError, refetch } = useHostsFile();
  const { save, download } = useHostsFileMutations();
  const [content, setContent] = useState('');
  const [url, setUrl] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (data) setContent(data.raw);
  }, [data]);

  const pageClass = useMemo(() => {
    const c = ['hostsfile-page'];
    if (isDark) c.push('is-dark');
    if (isUltra) c.push('is-ultra');
    return c.join(' ');
  }, [isDark, isUltra]);

  const columns: TableColumnsType<{ ip: string; domain: string }> = [
    { title: <Text type="secondary">{t('pages.hostsfile.colIp')}</Text>, dataIndex: 'ip', key: 'ip' },
    { title: <Text type="secondary">{t('pages.hostsfile.colDomain')}</Text>, dataIndex: 'domain', key: 'domain' },
  ];

  const runSave = async () => {
    setBusy(true);
    try { await save(content); } finally { setBusy(false); await refetch(); }
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

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin spinning={isLoading}>
              <Title level={3}>{t('menu.hostsFile')}</Title>
              <Text type="secondary" style={{ display: 'block', marginBottom: 24 }}>{t('pages.hostsfile.desc')}</Text>

              {isError ? (
                <Result status="error" title={t('pages.hostsfile.getFailed')} />
              ) : (
                <>
                  <Card title={t('pages.hostsfile.editorTitle')} variant="borderless" style={{ marginBottom: 16 }}>
                    <Input.TextArea rows={14} value={content} onChange={(e) => setContent(e.target.value)} spellCheck={false} />
                    <Button type="primary" icon={<SaveOutlined />} loading={busy} onClick={() => void runSave()} style={{ marginTop: 16 }}>
                      {t('pages.hostsfile.saveBtn')}
                    </Button>
                  </Card>
                  <Card title={t('pages.hostsfile.downloadTitle')} variant="borderless" style={{ marginBottom: 16 }}>
                    <Space.Compact style={{ width: '100%' }}>
                      <Input
                        value={url}
                        onChange={(e) => setUrl(e.target.value)}
                        placeholder={t('pages.hostsfile.downloadPlaceholder')}
                        style={{ fontFamily: 'monospace' }}
                      />
                      <Button icon={<DownloadOutlined />} loading={busy} disabled={!url} onClick={() => void runDownload()}>
                        {t('pages.hostsfile.downloadBtn')}
                      </Button>
                    </Space.Compact>
                  </Card>
                  {data && data.entries.length > 0 && (
                    <Card title={t('pages.hostsfile.entriesTitle')} variant="borderless">
                      <Table rowKey={(r) => `${r.ip} ${r.domain}`} columns={columns} dataSource={data.entries} size="small" />
                    </Card>
                  )}
                </>
              )}
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}