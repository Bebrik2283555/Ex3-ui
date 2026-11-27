import { useEffect, useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Form, Input, InputNumber, Select, Space, Switch } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';
import { useFormContext, useWatch } from 'react-hook-form';

import { FormField } from '@/components/form/rhf';
import { useOutboundTags } from '@/api/queries/useOutboundTags';
import { Protocols } from '@/schemas/primitives';
import { RandomUtil } from '@/utils';

// qWDTT, olcRTC and CSQTT are served by external cores that never see the Xray
// config, so this module is the whole Protocol tab: no transport or TLS form
// applies to them. Sniffing does apply, but to the generated Xray bridge rather
// than to the core, so its tab is shared with every other protocol.
//
// olcRTC only offers the transports its provider can carry. Order matters: the
// first entry is what a provider change falls back to.
const TRANSPORTS_BY_PROVIDER: Record<string, readonly string[]> = {
  telemost: ['vp8channel', 'videochannel'],
  wbstream: ['vp8channel', 'seichannel', 'videochannel'],
  salutejazz: ['datachannel'],
  vkcalls: ['vp8channel'],
  mtslink: ['vp8channel'],
  max: ['datachannel'],
};
const DEFAULT_TRANSPORTS = ['datachannel', 'vp8channel', 'seichannel', 'videochannel'] as const;
const DEFAULT_PROVIDER = 'jitsi';
const DEFAULT_TRANSPORT = 'datachannel';

const ROOM_PLACEHOLDER_BY_PROVIDER: Record<string, string> = {
  jitsi: 'https://meet.jit.si/...',
  telemost: 'https://telemost.yandex.ru/j/...',
  salutejazz: 'code:password',
  vkcalls: 'https://vk.ru/call/join/...',
  mtslink: 'https://my.mts-link.ru/j/...',
  max: 'https://max.ru/joincall/...',
  wbstream: 'room id',
};

function invalidOlcrtcRoomKey(provider: string, raw: unknown): string | null {
  if (typeof raw !== 'string') return 'pages.inbounds.sidecar.requiredRoom';
  const room = raw.trim();
  if (room === '') return 'pages.inbounds.sidecar.requiredRoom';
  if (/[\r\n]/.test(room)) return 'pages.inbounds.sidecar.requiredRoom';
  switch (provider) {
    case 'vkcalls': {
      try {
        const u = new URL(room);
        const parts = u.pathname.replace(/^\/+|\/+$/g, '').split('/');
        if (
          u.protocol !== 'https:' ||
          (u.host !== 'vk.ru' && u.host !== 'vk.com') ||
          u.username !== '' ||
          u.password !== '' ||
          u.port !== '' ||
          u.search !== '' ||
          u.hash !== '' ||
          parts.length !== 3 ||
          parts[0] !== 'call' ||
          parts[1] !== 'join' ||
          parts[2] === ''
        ) {
          return 'pages.inbounds.sidecar.invalidRoomVkcalls';
        }
      } catch {
        return 'pages.inbounds.sidecar.invalidRoomVkcalls';
      }
      return null;
    }
    case 'salutejazz':
      return /^[a-z0-9]{4,16}:[a-z0-9]{4,16}$/.test(room)
        ? null
        : 'pages.inbounds.sidecar.invalidRoomSalutejazz';
    case 'mtslink': {
      try {
        const u = new URL(room);
        const parts = u.pathname.replace(/^\/+|\/+$/g, '').split('/');
        const jIdx = parts.indexOf('j');
        if (
          (u.protocol !== 'https:' && u.protocol !== 'http:') ||
          u.host === '' ||
          jIdx < 0 ||
          parts.length < jIdx + 3 ||
          !parts[jIdx + 1] ||
          !parts[jIdx + 2]
        ) {
          return 'pages.inbounds.sidecar.invalidRoomMtslink';
        }
      } catch {
        return 'pages.inbounds.sidecar.invalidRoomMtslink';
      }
      return null;
    }
    case 'jitsi': {
      const noScheme = room.replace(/^https?:\/\//i, '');
      const slash = noScheme.indexOf('/');
      if (slash <= 0 || noScheme.slice(slash + 1).replace(/^\/+|\/+$/g, '') === '') {
        return 'pages.inbounds.sidecar.invalidRoomJitsi';
      }
      return null;
    }
    default:
      return null;
  }
}

function transportsFor(provider: string, authToken?: string): readonly string[] {
  if (provider === 'wbstream' && typeof authToken === 'string' && authToken.trim() !== '') {
    return ['vp8channel', 'seichannel', 'videochannel', 'datachannel'];
  }
  return TRANSPORTS_BY_PROVIDER[provider] ?? DEFAULT_TRANSPORTS;
}

function isValidICELocalIPv4(raw: unknown): boolean {
  if (isBlank(raw)) return true;
  if (typeof raw !== 'string') return false;
  const trimmed = raw.trim().replace(/^::ffff:/i, '');
  const parts = trimmed.split('.');
  if (parts.length !== 4) return false;
  const octets = parts.map((p) => (/^\d{1,3}$/.test(p) ? Number(p) : -1));
  if (octets.some((n) => n < 0 || n > 255)) return false;
  const [a, b, c, d] = octets;
  if (a === 0 && b === 0 && c === 0 && d === 0) return false;
  if (a === 127) return false;
  if (a === 169 && b === 254) return false;
  if (a >= 224 && a <= 239) return false;
  if (a === 255 && b === 255 && c === 255 && d === 255) return false;
  return true;
}

function isBlank(value: unknown): boolean {
  return (
    value === undefined || value === null || (typeof value === 'string' && value.trim() === '')
  );
}

// host:port for a derived listener address; IPv6 literals need brackets so the
// result stays one unambiguous address.
function joinHostPort(host: string, port: number): string {
  const clean = host.trim();
  const bracketed = clean.includes(':') && !clean.startsWith('[') ? `[${clean}]` : clean;
  return `${bracketed}:${port}`;
}

export default function SidecarFields() {
  const { t } = useTranslation();
  const { control, setValue } = useFormContext();
  const protocol = (useWatch({ control, name: 'protocol' }) ?? '') as string;
  const isQwdtt = protocol === Protocols.QWDTT;
  const isOlcrtc = protocol === Protocols.OLCRTC;
  const isCsqtt = protocol === Protocols.CSQTT;

  const routeThroughXray = useWatch({ control, name: 'settings.sidecar.routeThroughXray' }) as
    | boolean
    | null
    | undefined;
  const listenAddr = useWatch({ control, name: 'settings.sidecar.listenAddr' });
  const subHost = useWatch({ control, name: 'settings.sidecar.subHost' });
  const dns = useWatch({ control, name: 'settings.sidecar.dns' });
  const provider = (useWatch({ control, name: 'settings.sidecar.provider' }) ??
    DEFAULT_PROVIDER) as string;
  const authToken = (useWatch({ control, name: 'settings.sidecar.authToken' }) ?? '') as string;
  const transport = (useWatch({ control, name: 'settings.sidecar.transport' }) ??
    DEFAULT_TRANSPORT) as string;
  // Legacy keys: the pre-`subHost` deployments stored the dial host under a
  // different name per core, and the backend drops those keys on save.
  const legacyPublicHost = useWatch({ control, name: 'settings.sidecar.publicHost' });
  const legacyPublicIp = useWatch({ control, name: 'settings.sidecar.publicIp' });
  const inboundListen = (useWatch({ control, name: 'listen' }) ?? '') as string;
  const inboundPort = useWatch({ control, name: 'port' }) as number | undefined;
  const { data: outboundTags } = useOutboundTags({ excludeBlackhole: true });

  // An absent switch means "on": seed it silently so an untouched edit keeps
  // routing through Xray without pretending the operator changed anything.
  useEffect(() => {
    if (!isBlank(routeThroughXray)) return;
    setValue('settings.sidecar.routeThroughXray', true, { shouldDirty: false });
  }, [routeThroughXray, setValue]);

  // The listener clients dial is never meant to be empty; re-derive it from the
  // inbound's own listen/port when it is.
  useEffect(() => {
    if (!isQwdtt && !isCsqtt) return;
    if (!isBlank(listenAddr)) return;
    const host = inboundListen.trim() || '0.0.0.0';
    const fallbackPort = isQwdtt ? 56000 : 46000;
    const port = typeof inboundPort === 'number' && inboundPort > 0 ? inboundPort : fallbackPort;
    setValue('settings.sidecar.listenAddr', joinHostPort(host, port), { shouldDirty: false });
  }, [isQwdtt, isCsqtt, listenAddr, inboundListen, inboundPort, setValue]);

  // The subscription host used to live under a per-core legacy key; carry the
  // stored value over so links keep working until the operator edits it.
  useEffect(() => {
    if (!isQwdtt && !isCsqtt) return;
    if (!isBlank(subHost)) return;
    const legacy = isQwdtt ? legacyPublicHost : legacyPublicIp;
    if (typeof legacy !== 'string' || legacy.trim() === '') return;
    setValue('settings.sidecar.subHost', legacy, { shouldDirty: false });
  }, [isQwdtt, isCsqtt, subHost, legacyPublicHost, legacyPublicIp, setValue]);

  // qWDTT takes exactly one DNS server, so a list collapses to its first token.
  useEffect(() => {
    if (!isQwdtt) return;
    const raw = typeof dns === 'string' ? dns : '';
    const first = raw.split(/[,;\s]+/).find((token) => token !== '');
    if (first === undefined || first === raw) return;
    setValue('settings.sidecar.dns', first, { shouldDirty: false });
  }, [isQwdtt, dns, setValue]);

  // A provider or token change drops transports it cannot carry; the operator's edit
  // caused it, so this one marks the form dirty.
  useEffect(() => {
    if (!isOlcrtc) return;
    const allowed = transportsFor(provider, authToken);
    if (allowed.includes(transport)) return;
    setValue('settings.sidecar.transport', allowed[0], { shouldDirty: true });
  }, [isOlcrtc, provider, authToken, transport, setValue]);

  const noticeKey = isQwdtt
    ? 'pages.inbounds.sidecar.qwdttNotice'
    : isOlcrtc
      ? 'pages.inbounds.sidecar.olcrtcNotice'
      : 'pages.inbounds.sidecar.csqttNotice';

  return (
    <>
      <Alert style={{ marginBottom: 16 }} type="info" showIcon title={t(noticeKey)} />

      <FormField
        name={['settings', 'sidecar', 'routeThroughXray']}
        label={t('pages.inbounds.sidecar.routeThroughXray')}
        tooltip={t('pages.inbounds.sidecar.routeThroughXrayTip')}
        valueProp="checked"
      >
        <Switch />
      </FormField>

      {isQwdtt && routeThroughXray !== false && (
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
              ...(outboundTags ?? []).map((tag) => ({ value: tag, label: tag })),
            ]}
          />
        </FormField>
      )}

      {isQwdtt && <QwdttSettingsFields />}
      {isOlcrtc && <OlcrtcSettingsFields />}
      {isCsqtt && <CsqttSettingsFields />}
    </>
  );
}

function QwdttSettingsFields() {
  const { t } = useTranslation();
  return (
    <>
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

function CsqttSettingsFields() {
  const { t } = useTranslation();
  return (
    <>
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

function OlcrtcSettingsFields() {
  const { t } = useTranslation();
  const uid = useId();
  const { control, setValue } = useFormContext();
  const provider = (useWatch({ control, name: 'settings.sidecar.provider' }) ??
    DEFAULT_PROVIDER) as string;
  const authToken = (useWatch({ control, name: 'settings.sidecar.authToken' }) ?? '') as string;
  const transport = (useWatch({ control, name: 'settings.sidecar.transport' }) ??
    DEFAULT_TRANSPORT) as string;
  const showAuthToken = provider === 'max' || provider === 'wbstream' || !isBlank(authToken);
  const roomPlaceholder =
    ROOM_PLACEHOLDER_BY_PROVIDER[provider] ?? 'https://meet.jit.si/room or room id';

  return (
    <>
      <FormField
        name={['settings', 'sidecar', 'provider']}
        label={t('pages.inbounds.sidecar.provider')}
        rules={{
          validate: (value) => !isBlank(value) || t('pages.inbounds.sidecar.requiredProvider'),
        }}
      >
        <Select
          options={[
            { value: 'jitsi', label: 'Jitsi Meet' },
            { value: 'telemost', label: 'Yandex Telemost' },
            { value: 'wbstream', label: 'WbStream' },
            { value: 'salutejazz', label: 'SaluteJazz' },
            { value: 'vkcalls', label: 'VK Calls' },
            { value: 'mtslink', label: 'MTS Link' },
            { value: 'max', label: 'MAX Calls' },
          ]}
        />
      </FormField>

      {showAuthToken && (
        <FormField
          name={['settings', 'sidecar', 'authToken']}
          label={t('pages.inbounds.sidecar.authToken')}
          tooltip={t('pages.inbounds.sidecar.authTokenTip')}
          rules={{
            validate: (value) =>
              provider !== 'max' ||
              !isBlank(value) ||
              t('pages.inbounds.sidecar.requiredAuthToken'),
          }}
        >
          <Input.Password />
        </FormField>
      )}

      <FormField
        name={['settings', 'sidecar', 'roomId']}
        label={t('pages.inbounds.sidecar.roomId')}
        rules={{
          validate: (value) => {
            const issueKey = invalidOlcrtcRoomKey(provider, value);
            return issueKey === null || t(issueKey);
          },
        }}
      >
        <Input placeholder={roomPlaceholder} />
      </FormField>

      <Form.Item
        htmlFor={`${uid}-cryptoKey`}
        label={t('pages.inbounds.sidecar.cryptoKey')}
        tooltip={t('pages.inbounds.sidecar.cryptoKeyTip')}
      >
        <Space.Compact block>
          <FormField name={['settings', 'sidecar', 'cryptoKey']} noStyle>
            <Input id={`${uid}-cryptoKey`} style={{ width: 'calc(100% - 32px)' }} />
          </FormField>
          <Button
            aria-label={t('regenerate')}
            icon={<ReloadOutlined aria-hidden />}
            onClick={() =>
              setValue('settings.sidecar.cryptoKey', RandomUtil.randomSeq(64, { type: 'hex' }))
            }
          />
        </Space.Compact>
      </Form.Item>

      <FormField
        name={['settings', 'sidecar', 'transport']}
        label={t('pages.inbounds.sidecar.transport')}
      >
        <Select
          options={transportsFor(provider, authToken).map((value) => ({ value, label: value }))}
        />
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
        name={['settings', 'sidecar', 'iceLocalIp']}
        label={t('pages.inbounds.sidecar.iceLocalIp')}
        tooltip={t('pages.inbounds.sidecar.iceLocalIpTip')}
        rules={{
          validate: (value) =>
            isValidICELocalIPv4(value) || t('pages.inbounds.sidecar.invalidIceLocalIp'),
        }}
      >
        <Input placeholder="203.0.113.10" />
      </FormField>

      <FormField
        name={['settings', 'sidecar', 'dtlsProfile']}
        label={t('pages.inbounds.sidecar.dtlsProfile')}
        tooltip={t('pages.inbounds.sidecar.dtlsProfileTip')}
      >
        <Select
          options={[
            { value: '', label: t('pages.inbounds.sidecar.dtlsProfileDefault') },
            { value: 'off', label: t('pages.inbounds.sidecar.dtlsProfileOff') },
            { value: 'chrome-linux-138-compat-v1', label: 'chrome-linux-138-compat-v1' },
          ]}
        />
      </FormField>

      <FormField
        name={['settings', 'sidecar', 'udpEnabled']}
        label={t('pages.inbounds.sidecar.udpEnabled')}
        tooltip={t('pages.inbounds.sidecar.udpEnabledTip')}
        valueProp="checked"
      >
        <Switch />
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
