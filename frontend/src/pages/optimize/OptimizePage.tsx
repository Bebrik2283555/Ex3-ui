import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Card, Checkbox, ConfigProvider, Divider, InputNumber, Layout, Result, Row, Col, Space, Spin, Typography, message } from 'antd';
import { CheckCircleOutlined, CloseCircleOutlined, ThunderboltOutlined } from '@ant-design/icons';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { useOptimizeStatus, useOptimizeMutations } from '@/api/queries/useSystemTools';

const { Text, Title } = Typography;

export default function OptimizePage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => { setMessageInstance(messageApi); }, [messageApi]);
  usePageTitle();

  const { data: status, isLoading, isError, refetch } = useOptimizeStatus();
  const { apply, revert } = useOptimizeMutations();
  const [dns, setDns] = useState(true);
  const [bbr, setBbr] = useState(true);
  const [tcp, setTcp] = useState(true);
  const [swap, setSwap] = useState(false);
  const [swapSize, setSwapSize] = useState(1024);
  const [busy, setBusy] = useState<string | null>(null);

  const pageClass = useMemo(() => {
    const c = ['optimize-page'];
    if (isDark) c.push('is-dark');
    if (isUltra) c.push('is-ultra');
    return c.join(' ');
  }, [isDark, isUltra]);

  const runApply = async () => {
    setBusy('apply');
    try { await apply({ dns, bbr, tcp, swap, swapSize }); } finally { setBusy(null); await refetch(); }
  };
  const runRevert = async () => {
    setBusy('revert');
    try { await revert(); } finally { setBusy(null); await refetch(); }
  };

  const renderStatus = () => {
    if (!status) return null;
    if (!status.platformSupported) {
      return <Result status="warning" title={status.error || t('pages.optimize.notSupported')} />;    }
    const item = (label: React.ReactNode, ok: boolean | undefined) => (
      <Col xs={24} sm={12} md={8}>
        <Space>
          {ok ? <CheckCircleOutlined style={{ color: '#52c41a' }} /> : <CloseCircleOutlined style={{ color: '#ff4d4f' }} />}
          <Text>{label}</Text>
        </Space>
      </Col>
    );
    return (
      <Row gutter={[16, 12]}>
        {item(t('pages.optimize.bbr'), status.bbrEnabled)}
        {item(t('pages.optimize.tcpBuffer'), status.tcpBufferTuned)}
        {item(`${t('pages.optimize.swap')} (${status.swapSizeMiB} MiB)`, status.swapEnabled && status.swapActive)}
        {item(`${t('pages.optimize.dns')} (${status.dnsResolvers.join(', ')})`, status.dnsSet)}
      </Row>
    );
  };

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin spinning={isLoading}>
              <Title level={3}>{t('menu.optimize')}</Title>
              <Text type="secondary" style={{ display: 'block', marginBottom: 24 }}>{t('pages.optimize.desc')}</Text>

              <Card title={t('pages.optimize.currentState')} variant="borderless" style={{ marginBottom: 16 }}>
                {isError ? <Result status="error" title={t('pages.optimize.loadFailed')} /> : renderStatus()}
              </Card>

              <Card
                title={<><ThunderboltOutlined /> {t('pages.optimize.applyTitle')}</>}
                variant="borderless"
                style={{ marginBottom: 16 }}
              >
                <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                  <Checkbox checked={dns} onChange={(e) => setDns(e.target.checked)}>{t('pages.optimize.dnsOption')}</Checkbox>
                  <Checkbox checked={bbr} onChange={(e) => setBbr(e.target.checked)}>{t('pages.optimize.bbrOption')}</Checkbox>
                  <Checkbox checked={tcp} onChange={(e) => setTcp(e.target.checked)}>{t('pages.optimize.tcpOption')}</Checkbox>
                  <Space>
                    <Checkbox checked={swap} onChange={(e) => setSwap(e.target.checked)}>{t('pages.optimize.swapOption')}</Checkbox>
                    {swap && (
                      <InputNumber min={64} step={128} value={swapSize} onChange={(v) => setSwapSize(Number(v) || 0)} addonAfter="MiB" />
                    )}
                  </Space>
                  <Divider style={{ margin: '8px 0' }} />
                  <Space>
                    <Button type="primary" loading={busy === 'apply'} onClick={() => void runApply()}>{t('pages.optimize.apply')}</Button>
                    <Button danger loading={busy === 'revert'} onClick={() => void runRevert()}>{t('pages.optimize.revert')}</Button>
                  </Space>
                </Space>
              </Card>
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}