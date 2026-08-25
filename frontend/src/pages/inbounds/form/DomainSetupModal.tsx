import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Input, Modal, Space, Tooltip, Typography } from 'antd';
import { QuestionCircleOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import type { TemplateContext } from '@/lib/xray/inbound-templates';

const DOMAIN_RE = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$/i;
const IPV4_RE = /^(\d{1,3}\.){3}\d{1,3}$/;

export const isIpv4 = (value: string) =>
  IPV4_RE.test(value) && value.split('.').every((octet) => Number(octet) >= 0 && Number(octet) <= 255);

interface ResolveResult {
  domain: string;
  ips?: string[];
  serverIp?: string;
  matched?: boolean;
}

interface IssueResult {
  domain: string;
  certFile: string;
  keyFile: string;
}

interface DomainSetupModalProps {
  open: boolean;
  onClose: () => void;
  onDone: (ctx: TemplateContext) => void;
}

export default function DomainSetupModal({ open, onClose, onDone }: DomainSetupModalProps) {
  const { t } = useTranslation();
  const [domain, setDomain] = useState('');
  const [checking, setChecking] = useState(false);
  const [issuing, setIssuing] = useState(false);
  const [resolve, setResolve] = useState<ResolveResult | null>(null);
  const [issueError, setIssueError] = useState('');

  const trimmed = domain.trim();
  const isIp = isIpv4(trimmed);
  const valid = isIp || (DOMAIN_RE.test(trimmed) && !isIpv4(trimmed));

  const checkDns = async () => {
    if (!valid || isIp) return;
    setChecking(true);
    setIssueError('');
    try {
      const msg = await HttpUtil.get<ResolveResult>('/panel/api/server/resolveDomain', { domain: trimmed }, { silent: true });
      if (!msg.success || !msg.obj) {
        setIssueError(msg.msg || t('pages.inbounds.form.templateIssueFailed'));
        setResolve(null);
        return;
      }
      setResolve(msg.obj);
    } finally {
      setChecking(false);
    }
  };

  const issue = async () => {
    if (!valid) return;
    setIssuing(true);
    setIssueError('');
    try {
      if (isIp) {
        onDone({
          domain: trimmed,
          certFile: `/root/cert/${trimmed}/fullchain.pem`,
          keyFile: `/root/cert/${trimmed}/privkey.pem`,
        });
        return;
      }
      const msg = await HttpUtil.post<IssueResult>('/panel/api/server/issueCertificate', { domain: trimmed }, { silent: true });
      if (!msg.success || !msg.obj) {
        setIssueError(msg.msg || t('pages.inbounds.form.templateIssueFailed'));
        return;
      }
      onDone({ domain: msg.obj.domain, certFile: msg.obj.certFile, keyFile: msg.obj.keyFile });
    } finally {
      setIssuing(false);
    }
  };

  const busy = checking || issuing;
  const dnsOk = resolve?.matched === true;
  const serverIp = resolve?.serverIp && resolve.serverIp !== 'N/A' ? resolve.serverIp : '';

  return (
    <Modal
      open={open}
      title={t('pages.inbounds.form.templateDomainTitle')}
      okText={null}
      cancelText={t('close')}
      onCancel={onClose}
      width={520}
      destroyOnHidden
      maskClosable={!busy}
    >
      <Space direction="vertical" style={{ width: '100%' }} size="middle">
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          {t('pages.inbounds.form.templateDomainHelp')}
        </Typography.Paragraph>
        <Space.Compact style={{ width: '100%' }}>
          <Input
            value={domain}
            disabled={busy}
            placeholder={isIp ? '178.253.44.8' : 'example.com'}
            onChange={(e) => {
              setDomain(e.target.value);
              setResolve(null);
              setIssueError('');
            }}
            suffix={
              !isIp && (
                <Tooltip
                  title={
                    <span>
                      {t('pages.inbounds.form.templateDnsHint')}{' '}
                      <a href="https://dnsexit.com" target="_blank" rel="noreferrer">
                        dnsexit.com
                      </a>
                    </span>
                  }
                >
                  <QuestionCircleOutlined style={{ color: 'rgba(128,128,128,0.65)' }} />
                </Tooltip>
              )
            }
          />
          {!isIp && (
            <Button type="primary" loading={checking} disabled={!valid || issuing} onClick={checkDns}>
              {t('pages.inbounds.form.templateCheckDns')}
            </Button>
          )}
        </Space.Compact>

        {isIp && (
          <Alert
            type="info"
            showIcon
            message={t('pages.inbounds.form.templateIpCertNote', {
              certFile: `/root/cert/${trimmed}/fullchain.pem`,
              keyFile: `/root/cert/${trimmed}/privkey.pem`,
            })}
          />
        )}

        {resolve && !dnsOk && (
          <Alert
            type="warning"
            showIcon
            message={t('pages.inbounds.form.templateDnsMismatch')}
            description={
              serverIp
                ? t('pages.inbounds.form.templateDnsMismatchDetail', { serverIp })
                : undefined
            }
          />
        )}

        {dnsOk && (
          <Alert type="success" showIcon message={t('pages.inbounds.form.templateDnsOk')} />
        )}

        {issueError && <Alert type="error" showIcon message={issueError} />}

        {(isIp || resolve) && (
          <Button type="primary" block loading={issuing} disabled={!isIp && !dnsOk} onClick={issue}>
            {issuing
              ? t('pages.inbounds.form.templateIssuing')
              : isIp
                ? t('pages.inbounds.form.templateUseCert')
                : t('pages.inbounds.form.templateIssueCert')}
          </Button>
        )}
        {!isIp && !resolve && (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {t('pages.inbounds.form.templateIssueNote')}
          </Typography.Text>
        )}
      </Space>
    </Modal>
  );
}