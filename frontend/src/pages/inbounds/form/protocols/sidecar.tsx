import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Divider, Form, Input, InputNumber, Select, Space, Switch } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useFormContext, useWatch } from 'react-hook-form';

import { FormField } from '@/components/form/rhf';
import { useOutboundTags } from '@/api/queries/useOutboundTags';
import { Protocols } from '@/schemas/primitives';

function randomHex(bytes: number): string {
  const buf = new Uint8Array(bytes);
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
    crypto.getRandomValues(buf);
  } else {
    for (let i = 0; i < buf.length; i += 1) buf[i] = Math.floor(Math.random() * 256);
  }
  return Array.from(buf, (b) => b.toString(16).padStart(2, '0')).join('');
}

function joinHostPort(host: string, port: number): string {
  const h = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
  return `${h}:${port}`;
}

// Settings for the sidecar-served protocols (qWDTT, olcRTC, CSQTT). These
// never reach the Xray config: each inbound owns one external process.
export default function SidecarFields() {
  const { t } = useTranslation();
  const { control, setValue } = useFormContext();
  const protocol = (useWatch({ control, name: 'protocol' }) ?? '') as string;
  const provider = (useWatch({ control, name: 'settings.sidecar.provider' }) ?? 'jitsi') as string;
  const transport = (useWatch({ control, name: 'settings.sidecar.transport' }) ?? 'datachannel') as
    | string
    | undefined;
  const cryptoKey = (useWatch({ control, name: 'settings.sidecar.cryptoKey' }) ?? '') as string;
  const routeThroughXray = useWatch({
    control,
    name: 'settings.sidecar.routeThroughXray',
  }) as boolean | undefined;
  const listenAddr = (useWatch({ control, name: 'settings.sidecar.listenAddr' }) ?? '') as string;
  const subHost = (useWatch({ control, name: 'settings.sidecar.subHost' }) ?? '') as string;
  const dns = (useWatch({ control, name: 'settings.sidecar.dns' }) ?? '') as string;
  const legacyPublicHost = (useWatch({ control, name: 'settings.sidecar.publicHost' }) ??
    '') as string;
  const legacyPublicIp = (useWatch({ control, name: 'settings.sidecar.publicIp' }) ?? '') as string;
  const mainListen = (useWatch({ control, name: 'listen' }) ?? '') as string;
  const mainPort = useWatch({ control, name: 'port' }) as number | undefined;
  const { data: outboundTags } = useOutboundTags({ excludeBlackhole: true });
  const outboundOptions = (outboundTags ?? []).map((tag) => ({ value: tag, label: tag }));

  const routeFields = (
    <>
      <FormField
        name={['settings', 'sidecar', 'routeThroughXray']}
        label={t('pages.inbounds.sidecar.routeThroughXray')}
        tooltip={t('pages.inbounds.sidecar.routeThroughXrayTip')}
        valueProp="checked"
      >
        <Switch />
      </FormField>
      {routeThroughXray && protocol === Protocols.QWDTT && (
        <FormField
          name={['settings', 'sidecar', 'outboundTag']}
          label={t('pages.inbounds.sidecar.outboundTag')}
          tooltip={t('pages.inbounds.sidecar.outboundTagTip')}
        >
          <Select
            allowClear
            showSearch
            placeholder={t('pages.inbounds.sidecar.outboundPlaceholder')}
            options={[
              { value: '', label: t('pages.inbounds.sidecar.outboundPlaceholder') },
              ...outboundOptions,
            ]}
          />
        </FormField>
      )}
    </>
  );

  // Sidecars route through Xray by default. Only a missing key is filled here,
  // so an operator who explicitly turned it off keeps that choice.
  useEffect(() => {
    if (
      protocol !== Protocols.QWDTT &&
      protocol !== Protocols.OLCRTC &&
      protocol !== Protocols.CSQTT
    ) {
      return;
    }
    if (routeThroughXray !== undefined) return;
    setValue('settings.sidecar.routeThroughXray', true, { shouldDirty: false });
  }, [protocol, routeThroughXray, setValue]);

  // Older inbounds stored the listener only in the generic Listen/Port fields.
  // Mirror it into the sidecar field once so the form shows the real address.
  useEffect(() => {
    if (protocol !== Protocols.QWDTT && protocol !== Protocols.CSQTT) return;
    if (String(listenAddr).trim() !== '') return;
    const fallbackPort = protocol === Protocols.QWDTT ? 56000 : 46000;
    const host = String(mainListen).trim() || '0.0.0.0';
    const port = Number(mainPort) > 0 ? Number(mainPort) : fallbackPort;
    setValue('settings.sidecar.listenAddr', joinHostPort(host, port), { shouldDirty: false });
  }, [protocol, listenAddr, mainListen, mainPort, setValue]);

  // The old forms had separate public-host fields; fold them into the single
  // subHost field before Zod drops the legacy keys on save.
  useEffect(() => {
    if (protocol !== Protocols.QWDTT && protocol !== Protocols.CSQTT) return;
    if (String(subHost).trim() !== '') return;
    const legacy = protocol === Protocols.QWDTT ? legacyPublicHost : legacyPublicIp;
    if (String(legacy).trim() === '') return;
    setValue('settings.sidecar.subHost', legacy, { shouldDirty: false });
  }, [protocol, subHost, legacyPublicHost, legacyPublicIp, setValue]);

  // qWDTT accepts a single resolver; trim legacy comma-separated values so the
  // form shows exactly what the process will receive.
  useEffect(() => {
    if (protocol !== Protocols.QWDTT) return;
    const first = String(dns)
      .split(/[,;\s]+/)
      .map((part) => part.trim())
      .find((part) => part !== '');
    if (!first || first === dns) return;
    setValue('settings.sidecar.dns', first, { shouldDirty: false });
  }, [protocol, dns, setValue]);

  // Telemost only carries VP8/video, WB Stream has no DataChannel.
  useEffect(() => {
    if (protocol !== Protocols.OLCRTC) return;
    const allowed =
      provider === 'telemost'
        ? ['vp8channel', 'videochannel']
        : provider === 'wbstream'
          ? ['vp8channel', 'seichannel', 'videochannel']
          : ['datachannel', 'vp8channel', 'seichannel', 'videochannel'];
    if (!transport || allowed.includes(transport)) return;
    setValue('settings.sidecar.transport', allowed[0], { shouldDirty: true });
  }, [protocol, provider, transport, setValue]);

  if (protocol === Protocols.QWDTT) {
    return (
      <>
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          title={t('pages.inbounds.sidecar.qwdttNotice')}
        />
        {routeFields}
        <FormField
          name={['settings', 'sidecar', 'listenAddr']}
          label={t('pages.inbounds.sidecar.listenDtls')}
          tooltip={t('pages.inbounds.sidecar.listenDtlsTip')}
        >
          <Input placeholder="0.0.0.0:56000" />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'wgPort']}
          label={t('pages.inbounds.sidecar.wgPort')}
        >
          <InputNumber min={1} max={65535} style={{ width: '100%' }} />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'dns']}
          label={t('pages.inbounds.sidecar.dnsList')}
          tooltip={t('pages.inbounds.sidecar.dnsListTip')}
        >
          <Input placeholder="1.1.1.1" />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'subHost']}
          label={t('pages.inbounds.sidecar.publicHost')}
          tooltip={t('pages.inbounds.sidecar.publicHostTip')}
        >
          <Input placeholder="vpn.example.com:56000" />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'hashes']}
          label={t('pages.inbounds.sidecar.vkHashes')}
          tooltip={t('pages.inbounds.sidecar.vkHashesTip')}
        >
          <Input.TextArea rows={2} />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'listenRaw']}
          label={t('pages.inbounds.sidecar.listenRaw')}
          tooltip={t('pages.inbounds.sidecar.listenRawTip')}
        >
          <Input placeholder="0.0.0.0:56003" />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'listenDirect']}
          label={t('pages.inbounds.sidecar.listenDirect')}
          tooltip={t('pages.inbounds.sidecar.listenDirectTip')}
        >
          <Input placeholder="0.0.0.0:56004" />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'clientPort']}
          label={t('pages.inbounds.sidecar.clientPort')}
        >
          <InputNumber min={1} max={65535} style={{ width: '100%' }} />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'workers']}
          label={t('pages.inbounds.sidecar.workers')}
        >
          <InputNumber min={1} max={256} style={{ width: '100%' }} />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'configDir']}
          label={t('pages.inbounds.sidecar.configDir')}
          tooltip={t('pages.inbounds.sidecar.configDirTip')}
          extra={t('pages.inbounds.sidecar.configDirWarning')}
        >
          <Input placeholder={t('pages.inbounds.sidecar.configDirPlaceholder')} />
        </FormField>
      </>
    );
  }

  if (protocol === Protocols.OLCRTC) {
    const transportOptions =
      provider === 'telemost'
        ? [
            { value: 'vp8channel', label: 'vp8channel' },
            { value: 'videochannel', label: 'videochannel' },
          ]
        : provider === 'wbstream'
          ? [
              { value: 'vp8channel', label: 'vp8channel' },
              { value: 'seichannel', label: 'seichannel' },
              { value: 'videochannel', label: 'videochannel' },
            ]
          : [
              { value: 'datachannel', label: 'datachannel' },
              { value: 'vp8channel', label: 'vp8channel' },
              { value: 'seichannel', label: 'seichannel' },
              { value: 'videochannel', label: 'videochannel' },
            ];

    return (
      <>
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          title={t('pages.inbounds.sidecar.olcrtcNotice')}
        />
        {routeFields}
        <FormField
          name={['settings', 'sidecar', 'provider']}
          label={t('pages.inbounds.sidecar.provider')}
          rules={{ required: t('pages.inbounds.sidecar.requiredProvider') }}
        >
          <Select
            options={[
              { value: 'jitsi', label: 'Jitsi Meet' },
              { value: 'telemost', label: 'Yandex Telemost' },
              { value: 'wbstream', label: 'WbStream' },
            ]}
          />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'roomId']}
          label={t('pages.inbounds.sidecar.roomId')}
          rules={{ required: t('pages.inbounds.sidecar.requiredRoom') }}
        >
          <Input placeholder="https://meet.jit.si/room or room id" />
        </FormField>
        <Form.Item
          label={t('pages.inbounds.sidecar.cryptoKey')}
          tooltip={t('pages.inbounds.sidecar.cryptoKeyTip')}
        >
          <Space.Compact style={{ display: 'flex' }}>
            <Input
              aria-label={t('pages.inbounds.sidecar.cryptoKey')}
              value={cryptoKey}
              style={{ flex: 1 }}
              onChange={(e) => setValue('settings.sidecar.cryptoKey', e.target.value)}
            />
            <Button
              aria-label={t('regenerate')}
              icon={<ReloadOutlined aria-hidden />}
              onClick={() => setValue('settings.sidecar.cryptoKey', randomHex(32))}
            />
          </Space.Compact>
        </Form.Item>
        <FormField
          name={['settings', 'sidecar', 'transport']}
          label={t('pages.inbounds.sidecar.transport')}
        >
          <Select options={transportOptions} />
        </FormField>
        {transport === 'vp8channel' && (
          <>
            <FormField
              name={['settings', 'sidecar', 'vp8Fps']}
              label={t('pages.inbounds.sidecar.vp8Fps')}
            >
              <InputNumber min={1} max={120} style={{ width: '100%' }} />
            </FormField>
            <FormField
              name={['settings', 'sidecar', 'vp8Batch']}
              label={t('pages.inbounds.sidecar.vp8Batch')}
            >
              <InputNumber min={1} max={64} style={{ width: '100%' }} />
            </FormField>
          </>
        )}
        {transport === 'seichannel' && (
          <>
            <FormField
              name={['settings', 'sidecar', 'seiFps']}
              label={t('pages.inbounds.sidecar.seiFps')}
            >
              <InputNumber min={1} max={120} style={{ width: '100%' }} />
            </FormField>
            <FormField
              name={['settings', 'sidecar', 'seiBatch']}
              label={t('pages.inbounds.sidecar.seiBatch')}
            >
              <InputNumber min={1} max={64} style={{ width: '100%' }} />
            </FormField>
            <FormField
              name={['settings', 'sidecar', 'seiFrag']}
              label={t('pages.inbounds.sidecar.seiFrag')}
            >
              <InputNumber min={1} style={{ width: '100%' }} />
            </FormField>
            <FormField
              name={['settings', 'sidecar', 'seiAck']}
              label={t('pages.inbounds.sidecar.seiAck')}
            >
              <InputNumber min={1} style={{ width: '100%' }} />
            </FormField>
          </>
        )}
        {transport === 'videochannel' && (
          <>
            <FormField
              name={['settings', 'sidecar', 'videoW']}
              label={t('pages.inbounds.sidecar.videoW')}
            >
              <InputNumber min={1} style={{ width: '100%' }} />
            </FormField>
            <FormField
              name={['settings', 'sidecar', 'videoH']}
              label={t('pages.inbounds.sidecar.videoH')}
            >
              <InputNumber min={1} style={{ width: '100%' }} />
            </FormField>
            <FormField
              name={['settings', 'sidecar', 'videoFps']}
              label={t('pages.inbounds.sidecar.videoFps')}
            >
              <InputNumber min={1} max={120} style={{ width: '100%' }} />
            </FormField>
            <FormField
              name={['settings', 'sidecar', 'videoCodec']}
              label={t('pages.inbounds.sidecar.videoCodec')}
            >
              <Select
                options={[
                  { value: 'qrcode', label: 'QR' },
                  { value: 'tile', label: 'Tile' },
                ]}
              />
            </FormField>
          </>
        )}
        <FormField
          name={['settings', 'sidecar', 'dns']}
          label={t('pages.inbounds.sidecar.dnsResolver')}
          tooltip={t('pages.inbounds.sidecar.dnsResolverTip')}
        >
          <Input placeholder="8.8.8.8:53" />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'debug']}
          label={t('pages.inbounds.sidecar.debug')}
          tooltip={t('pages.inbounds.sidecar.debugTip')}
          valueProp="checked"
        >
          <Switch />
        </FormField>
      </>
    );
  }

  if (protocol === Protocols.CSQTT) {
    return (
      <>
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          title={t('pages.inbounds.sidecar.csqttNotice')}
        />
        {routeFields}
        <FormField
          name={['settings', 'sidecar', 'listenAddr']}
          label={t('pages.inbounds.sidecar.listenUdp')}
          tooltip={t('pages.inbounds.sidecar.listenUdpTip')}
        >
          <Input placeholder="0.0.0.0:46000" />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'subHost']}
          label={t('pages.inbounds.sidecar.publicHost')}
          tooltip={t('pages.inbounds.sidecar.publicHostTip')}
        >
          <Input placeholder="vpn.example.com" />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'hashes']}
          label={t('pages.inbounds.sidecar.vkHashes')}
        >
          <Input.TextArea rows={2} />
        </FormField>
        <FormField
          name={['settings', 'sidecar', 'configDir']}
          label={t('pages.inbounds.sidecar.configDir')}
          tooltip={t('pages.inbounds.sidecar.configDirTip')}
          extra={t('pages.inbounds.sidecar.configDirWarning')}
        >
          <Input placeholder={t('pages.inbounds.sidecar.configDirPlaceholder')} />
        </FormField>
      </>
    );
  }

  return <Divider />;
}
