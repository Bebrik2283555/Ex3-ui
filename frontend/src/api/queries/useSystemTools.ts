import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

export type CoreName = 'qwdtt' | 'olcrtc';

export interface WDTTClient {
  name: string;
  subscriptionName: string;
  subscriptionDescription: string;
  password: string;
  vkHashes: string;
  enabled: boolean;
  subUri: string;
}

export interface ExtraConfig {
  enabled?: boolean;
  autoStart?: boolean;
  binaryPath?: string;
  listenAddr?: string;
  wgPort?: number;
  password?: string;
  dns?: string;
  configDir?: string;
  listenRaw?: string;
  subToken?: string;
  subHost?: string;
  vkHashes?: string;
  clients?: WDTTClient[];
  adminId?: string;
  botToken?: string;
  configFile?: string;
  dataDir?: string;
  provider?: string;
  roomId?: string;
  cryptoKey?: string;
  transport?: string;
  olcrtcDns?: string;
  vp8Fps?: number;
  vp8Batch?: number;
  ofConfigFile?: string;
  ofTransport?: string;
  ofMode?: string;
  ofUrl?: string;
  ofDirectListen?: string;
  ofEncryptionKey?: string;
  ofNegotiate?: boolean;
  ofShareHost?: string;
  ofCodec?: string;
  ofMaxToken?: string;
  ofMaxUid?: string;
  ofTransports?: string;
  ofYandexUrl?: string;
  ofVyandexUrl?: string;
  ofMailruUrl?: string;
  ofCupsonlineUrl?: string;
  debug?: boolean;
  extraArgs?: string;
}

export interface ExtraStatus {
  name: CoreName;
  displayName: string;
  enabled: boolean;
  running: boolean;
  binaryExists: boolean;
  error?: string;
  lastLog?: string;
  logs?: string[];
  config?: ExtraConfig;
  connectUri?: string;
}

export interface OptimizeStatus {
  bbrEnabled: boolean;
  tcpBufferTuned: boolean;
  swapEnabled: boolean;
  swapSizeMiB: number;
  swapActive: boolean;
  dnsSet: boolean;
  dnsResolvers: string[];
  platformSupported: boolean;
  error?: string;
}

export const EMPTY_OPTIMIZE_STATUS: OptimizeStatus = {
  bbrEnabled: false,
  tcpBufferTuned: false,
  swapEnabled: false,
  swapSizeMiB: 0,
  swapActive: false,
  dnsSet: false,
  dnsResolvers: [],
  platformSupported: false,
};

export interface OptimizeApplyOptions {
  dns: boolean;
  bbr: boolean;
  tcp: boolean;
  swap: boolean;
  swapSize: number;
}

export interface ZapretStatus {
  installed: boolean;
  running: boolean;
  firewall: string;
  enabled: boolean;
  error?: string;
}

export const EMPTY_ZAPRET_STATUS: ZapretStatus = {
  installed: false,
  running: false,
  firewall: '',
  enabled: false,
};

export interface ZapretHosts {
  bypass: string[];
  ignore: string[];
}

export interface HostsFile {
  entries: { ip: string; domain: string }[];
  raw: string;
}

const queryKeys = keys as Record<string, unknown>;

export function useExtrasStatus() {
  return useQuery({
    queryKey: ['extras', 'services'],
    queryFn: async (): Promise<ExtraStatus[]> => {
      const msg = await HttpUtil.get<ExtraStatus[]>('/panel/api/extra/services', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch services');
      return msg.obj ?? [];
    },
  });
}

export function useExtrasMutations() {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['extras'] });
  void queryKeys;

  const start = useMutation({
    mutationFn: (name: CoreName) => HttpUtil.post(`/panel/api/extra/services/${name}/start`),
    onSuccess: () => invalidate(),
  });
  const stop = useMutation({
    mutationFn: (name: CoreName) => HttpUtil.post(`/panel/api/extra/services/${name}/stop`),
    onSuccess: () => invalidate(),
  });
  const restart = useMutation({
    mutationFn: (name: CoreName) => HttpUtil.post(`/panel/api/extra/services/${name}/restart`),
    onSuccess: () => invalidate(),
  });
  const saveConfig = useMutation({
    mutationFn: ({ name, cfg }: { name: CoreName; cfg: ExtraConfig }) =>
      HttpUtil.put(`/panel/api/extra/services/${name}/config`, cfg, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const uploadBinary = useMutation({
    mutationFn: ({ name, file }: { name: CoreName; file: File }) => {
      const form = new FormData();
      form.append('file', file);
      return HttpUtil.post(`/panel/api/extra/services/${name}/upload`, form);
    },
    onSuccess: () => invalidate(),
  });
  const downloadBinary = useMutation({
    mutationFn: ({ name, url }: { name: CoreName; url: string }) =>
      HttpUtil.post(`/panel/api/extra/services/${name}/download`, { url }, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const deleteBinary = useMutation({
    mutationFn: (name: CoreName) => HttpUtil.delete(`/panel/api/extra/services/${name}/binary`),
    onSuccess: () => invalidate(),
  });

  return {
    start: (name: CoreName) => start.mutateAsync(name),
    stop: (name: CoreName) => stop.mutateAsync(name),
    restart: (name: CoreName) => restart.mutateAsync(name),
    saveConfig: (name: CoreName, cfg: ExtraConfig) => saveConfig.mutateAsync({ name, cfg }),
    uploadBinary: (name: CoreName, file: File) => uploadBinary.mutateAsync({ name, file }),
    downloadBinary: (name: CoreName, url: string) => downloadBinary.mutateAsync({ name, url }),
    deleteBinary: (name: CoreName) => deleteBinary.mutateAsync(name),
  };
}

export function useOptimizeStatus() {
  return useQuery({
    queryKey: ['optimize', 'status'],
    queryFn: async (): Promise<OptimizeStatus> => {
      const msg = await HttpUtil.get<OptimizeStatus>('/panel/api/optimize/status', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch optimize status');
      return msg.obj ?? EMPTY_OPTIMIZE_STATUS;
    },
  });
}

export function useOptimizeMutations() {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['optimize'] });

  const apply = useMutation({
    mutationFn: (opts: OptimizeApplyOptions) =>
      HttpUtil.post('/panel/api/optimize/apply', opts, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const revert = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/optimize/revert'),
    onSuccess: () => invalidate(),
  });

  return {
    apply: (opts: OptimizeApplyOptions) => apply.mutateAsync(opts),
    revert: () => revert.mutateAsync(),
  };
}

export function useZapretStatus() {
  return useQuery({
    queryKey: ['zapret', 'status'],
    queryFn: async (): Promise<ZapretStatus> => {
      const msg = await HttpUtil.get<ZapretStatus>('/panel/api/zapret/status', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch zapret status');
      return msg.obj ?? EMPTY_ZAPRET_STATUS;
    },
  });
}

export function useZapretHosts() {
  return useQuery({
    queryKey: ['zapret', 'hosts'],
    queryFn: async (): Promise<ZapretHosts> => {
      const msg = await HttpUtil.get<ZapretHosts>('/panel/api/zapret/hosts', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch zapret hosts');
      return msg.obj ?? { bypass: [], ignore: [] };
    },
  });
}

export function useZapretFiles() {
  return useQuery({
    queryKey: ['zapret', 'files'],
    queryFn: async (): Promise<Record<string, string>> => {
      const msg = await HttpUtil.get<Record<string, string>>('/panel/api/zapret/files', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch zapret files');
      return msg.obj ?? {};
    },
    enabled: false,
  });
}

export function useZapretMutations() {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['zapret'] });

  const downloadInstall = useMutation({
    mutationFn: (cfg: { url: string; firewall: string; ifaceWan: string; ifaceLan: string }) =>
      HttpUtil.post('/panel/api/zapret/download', cfg, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const uninstall = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/zapret/uninstall'),
    onSuccess: () => invalidate(),
  });
  const start = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/zapret/start'),
    onSuccess: () => invalidate(),
  });
  const stop = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/zapret/stop'),
    onSuccess: () => invalidate(),
  });
  const restart = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/zapret/restart'),
    onSuccess: () => invalidate(),
  });
  const saveHosts = useMutation({
    mutationFn: (hosts: ZapretHosts) =>
      HttpUtil.put('/panel/api/zapret/hosts', hosts, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const saveConfig = useMutation({
    mutationFn: (content: string) =>
      HttpUtil.put('/panel/api/zapret/config', { name: 'config.txt', content }, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const saveListFile = useMutation({
    mutationFn: ({ name, content }: { name: string; content: string }) =>
      HttpUtil.put('/panel/api/zapret/file', { name, content }, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const restoreZip = useMutation({
    mutationFn: (file: File) => {
      const form = new FormData();
      form.append('file', file);
      return HttpUtil.post('/panel/api/zapret/restore', form);
    },
    onSuccess: () => invalidate(),
  });

  return {
    downloadInstall: (cfg: { url: string; firewall: string; ifaceWan: string; ifaceLan: string }) =>
      downloadInstall.mutateAsync(cfg),
    uninstall: () => uninstall.mutateAsync(),
    start: () => start.mutateAsync(),
    stop: () => stop.mutateAsync(),
    restart: () => restart.mutateAsync(),
    saveHosts: (hosts: ZapretHosts) => saveHosts.mutateAsync(hosts),
    saveConfig: (content: string) => saveConfig.mutateAsync(content),
    saveListFile: (name: string, content: string) => saveListFile.mutateAsync({ name, content }),
    restoreZip: (file: File) => restoreZip.mutateAsync(file),
  };
}

export function useHostsFile() {
  return useQuery({
    queryKey: ['hostsfile'],
    queryFn: async (): Promise<HostsFile> => {
      const msg = await HttpUtil.get<HostsFile>('/panel/api/hostsfile', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch hosts file');
      return msg.obj ?? { entries: [], raw: '' };
    },
  });
}

export function useHostsFileMutations() {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['hostsfile'] });

  const save = useMutation({
    mutationFn: (raw: string) => HttpUtil.put('/panel/api/hostsfile', { raw }, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const download = useMutation({
    mutationFn: (url: string) =>
      HttpUtil.post('/panel/api/hostsfile/download', { url }, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });

  return {
    save: (raw: string) => save.mutateAsync(raw),
    download: (url: string) => download.mutateAsync(url),
  };
}

export interface DnsResolverEntry {
  domain: string;
  ip: string;
  status: string;
}

export interface DnsResolverSettings {
  enabled: boolean;
  dnsServer: string;
  intervalHours: number;
  domains: string[];
}

export interface DnsTestResult {
  success: boolean;
  latencyMs: number;
  ip?: string;
  server?: string;
  error?: string;
}

export interface DnsResolverStatus {
  enabled: boolean;
  dnsServer: string;
  intervalHours: number;
  domains: string[];
  defaultDomains?: string[];
  running: boolean;
  lastRun: string;
  lastResult: string;
  lastError: string;
  entries: DnsResolverEntry[];
}

export const EMPTY_DNS_RESOLVER_STATUS: DnsResolverStatus = {
  enabled: false,
  dnsServer: 'https://dns.comss.one/dns-query',
  intervalHours: 12,
  domains: [],
  defaultDomains: [],
  running: false,
  lastRun: '',
  lastResult: '',
  lastError: '',
  entries: [],
};

export function useDnsResolverStatus(refetchInterval?: number) {
  return useQuery({
    queryKey: ['dnsresolver', 'status'],
    queryFn: async (): Promise<DnsResolverStatus> => {
      const msg = await HttpUtil.get<DnsResolverStatus>(
        '/panel/api/dnsresolver/status',
        undefined,
        {
          silent: true,
        },
      );
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch DNS resolver status');
      return msg.obj ?? EMPTY_DNS_RESOLVER_STATUS;
    },
    refetchInterval,
  });
}

export function useDnsResolverMutations() {
  const queryClient = useQueryClient();
  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['dnsresolver'] });
    queryClient.invalidateQueries({ queryKey: ['hostsfile'] });
  };

  const saveSettings = useMutation({
    mutationFn: (settings: DnsResolverSettings) =>
      HttpUtil.post('/panel/api/dnsresolver/settings', settings, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });

  const saveDomains = useMutation({
    mutationFn: (domains: string[]) =>
      HttpUtil.post('/panel/api/dnsresolver/domains', { domains }, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });

  const resolve = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/dnsresolver/resolve'),
    onSuccess: () => invalidate(),
  });

  const testDns = useMutation({
    mutationFn: (dnsServer: string) =>
      HttpUtil.post<DnsTestResult>(
        '/panel/api/dnsresolver/test',
        { dnsServer },
        { ...JSON_HEADERS, silent: true },
      ),
  });

  return {
    saveSettings: (settings: DnsResolverSettings) => saveSettings.mutateAsync(settings),
    saveDomains: (domains: string[]) => saveDomains.mutateAsync(domains),
    resolve: () => resolve.mutateAsync(),
    testDns: (dnsServer: string) => testDns.mutateAsync(dnsServer),
  };
}

export interface WarpStatus {
  installed: boolean;
  running: boolean;
  watchdogActive: boolean;
  installing: boolean;
  rotating: boolean;
  error?: string;
  ip?: string;
  country?: string;
}

export const EMPTY_WARP_STATUS: WarpStatus = {
  installed: false,
  running: false,
  watchdogActive: false,
  installing: false,
  rotating: false,
};

export interface WarpSettings {
  domains: string[];
  timeout: number;
  checkInterval: number;
  checkUrl: string;
  autoRotate: boolean;
}

export const DEFAULT_WARP_SETTINGS: WarpSettings = {
  domains: [],
  timeout: 5,
  checkInterval: 5,
  checkUrl: 'https://api.telegram.org/',
  autoRotate: true,
};

export function useWarpStatus(refetchInterval?: number) {
  return useQuery({
    queryKey: ['warp', 'status'],
    queryFn: async (): Promise<WarpStatus> => {
      const msg = await HttpUtil.get<WarpStatus>('/panel/api/warp/status', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch warp status');
      return msg.obj ?? EMPTY_WARP_STATUS;
    },
    refetchInterval,
  });
}

export function useWarpSettings(enabled = true) {
  return useQuery({
    queryKey: ['warp', 'settings'],
    queryFn: async (): Promise<WarpSettings> => {
      const msg = await HttpUtil.get<WarpSettings>('/panel/api/warp/settings', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch warp settings');
      return msg.obj ?? DEFAULT_WARP_SETTINGS;
    },
    enabled,
  });
}

export const DEFAULT_TG_DOMAINS = [
  't.me',
  'tg.dev',
  'tg.org',
  'tx.me',
  'teleg.xyz',
  'telegram.ai',
  'telegram.asia',
  'telegram.biz',
  'telegram.cloud',
  'telegram.cn',
  'telegram.co',
  'telegram.com',
  'telegram.de',
  'telegram.dev',
  'telegram.dog',
  'telegram.eu',
  'telegram.fr',
  'telegram.host',
  'telegram.in',
  'telegram.info',
  'telegram.io',
  'telegram.jp',
  'telegram.me',
  'telegram.net',
  'telegram.org',
  'telegram.qa',
  'telegram.ru',
  'telegram.services',
  'telegram.solutions',
  'telegram.space',
  'telegram.team',
  'telegram.tech',
  'telegram.uk',
  'telegram.us',
  'telegram.website',
  'telegram.xyz',
  'telegramapp.org',
  'telegra.ph',
  'telesco.pe',
  'nicegram.app',
  'telegramdownload.com',
  'cdn-telegram.org',
  'comments.app',
  'contest.com',
  'fragment.com',
  'graph.org',
  'quiz.directory',
  'tdesktop.com',
  'telega.one',
  'telegram-cdn.org',
  'usercontent.dev',
  'tgram.org',
  'torg.org',
];

export function isDefaultTelegramDomains(domains?: string[]): boolean {
  if (!domains || domains.length === 0) return true;
  if (domains.length === 1 && domains[0] === 'api.telegram.org') return true;
  if (domains.length === DEFAULT_TG_DOMAINS.length) {
    const set = new Set(domains);
    return DEFAULT_TG_DOMAINS.every((d) => set.has(d));
  }
  return false;
}

export function useWarpMutations() {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['warp'] });

  const install = useMutation({
    mutationFn: (settings?: WarpSettings) =>
      HttpUtil.post('/panel/api/warp/install', settings, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const uninstall = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/warp/uninstall'),
    onSuccess: () => invalidate(),
  });
  const start = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/warp/start'),
    onSuccess: () => invalidate(),
  });
  const stop = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/warp/stop'),
    onSuccess: () => invalidate(),
  });
  const restart = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/warp/restart'),
    onSuccess: () => invalidate(),
  });
  const rotate = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/warp/rotate'),
    onSuccess: () => invalidate(),
  });
  const saveSettings = useMutation({
    mutationFn: (settings: WarpSettings) =>
      HttpUtil.post('/panel/api/warp/settings', settings, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });

  return {
    install: (settings?: WarpSettings) => install.mutateAsync(settings),
    uninstall: () => uninstall.mutateAsync(),
    start: () => start.mutateAsync(),
    stop: () => stop.mutateAsync(),
    restart: () => restart.mutateAsync(),
    rotate: () => rotate.mutateAsync(),
    saveSettings: (settings: WarpSettings) => saveSettings.mutateAsync(settings),
  };
}

export function useWarpLogs(enabled = false) {
  return useQuery({
    queryKey: ['warp', 'logs'],
    queryFn: async (): Promise<string[]> => {
      const msg = await HttpUtil.get<string[]>('/panel/api/warp/logs', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch warp logs');
      return msg.obj ?? [];
    },
    enabled,
    refetchInterval: enabled ? 5000 : undefined,
  });
}

export function useWarpInstallLogs(refetchInterval?: number) {
  return useQuery({
    queryKey: ['warp', 'install-logs'],
    queryFn: async (): Promise<string[]> => {
      const msg = await HttpUtil.get<string[]>('/panel/api/warp/install-logs', undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch warp install logs');
      return msg.obj ?? [];
    },
    refetchInterval,
  });
}
