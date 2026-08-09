import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

export type CoreName = 'qwdtt' | 'olcrtc';

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
  configFile?: string;
  dataDir?: string;
  provider?: string;
  roomId?: string;
  cryptoKey?: string;
  transport?: string;
  olcrtcDns?: string;
  vp8Fps?: number;
  vp8Batch?: number;
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
      const msg = await HttpUtil.get<ExtraStatus[]>('/panel/api/extra/services', undefined, { silent: true });
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
      const msg = await HttpUtil.get<OptimizeStatus>('/panel/api/optimize/status', undefined, { silent: true });
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
      const msg = await HttpUtil.get<ZapretStatus>('/panel/api/zapret/status', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch zapret status');
      return msg.obj ?? EMPTY_ZAPRET_STATUS;
    },
  });
}

export function useZapretHosts() {
  return useQuery({
    queryKey: ['zapret', 'hosts'],
    queryFn: async (): Promise<ZapretHosts> => {
      const msg = await HttpUtil.get<ZapretHosts>('/panel/api/zapret/hosts', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch zapret hosts');
      return msg.obj ?? { bypass: [], ignore: [] };
    },
  });
}

export function useZapretFiles() {
  return useQuery({
    queryKey: ['zapret', 'files'],
    queryFn: async (): Promise<Record<string, string>> => {
      const msg = await HttpUtil.get<Record<string, string>>('/panel/api/zapret/files', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch zapret files');
      return msg.obj ?? {};
    },
    enabled: false,
  });
}

export function useZapretMutations() {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['zapret'] });

  const install = useMutation({
    mutationFn: (cfg: { firewall: string; ifaceWan: string; ifaceLan: string }) =>
      HttpUtil.post('/panel/api/zapret/install', cfg, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const downloadInstall = useMutation({
    mutationFn: (cfg: { url: string; firewall: string; ifaceWan: string; ifaceLan: string }) =>
      HttpUtil.post('/panel/api/zapret/download', cfg, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });
  const uninstall = useMutation({
    mutationFn: () => HttpUtil.post('/panel/api/zapret/uninstall'),
    onSuccess: () => invalidate(),
  });
  const start = useMutation({ mutationFn: () => HttpUtil.post('/panel/api/zapret/start'), onSuccess: () => invalidate() });
  const stop = useMutation({ mutationFn: () => HttpUtil.post('/panel/api/zapret/stop'), onSuccess: () => invalidate() });
  const restart = useMutation({ mutationFn: () => HttpUtil.post('/panel/api/zapret/restart'), onSuccess: () => invalidate() });
  const saveHosts = useMutation({
    mutationFn: (hosts: ZapretHosts) => HttpUtil.put('/panel/api/zapret/hosts', hosts, JSON_HEADERS),
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

  return {
    install: (cfg: { firewall: string; ifaceWan: string; ifaceLan: string }) => install.mutateAsync(cfg),
    downloadInstall: (cfg: { url: string; firewall: string; ifaceWan: string; ifaceLan: string }) =>
      downloadInstall.mutateAsync(cfg),
    uninstall: () => uninstall.mutateAsync(),
    start: () => start.mutateAsync(),
    stop: () => stop.mutateAsync(),
    restart: () => restart.mutateAsync(),
    saveHosts: (hosts: ZapretHosts) => saveHosts.mutateAsync(hosts),
    saveConfig: (content: string) => saveConfig.mutateAsync(content),
    saveListFile: (name: string, content: string) => saveListFile.mutateAsync({ name, content }),
  };
}

export function useHostsFile() {
  return useQuery({
    queryKey: ['hostsfile'],
    queryFn: async (): Promise<HostsFile> => {
      const msg = await HttpUtil.get<HostsFile>('/panel/api/hostsfile', undefined, { silent: true });
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
    mutationFn: (url: string) => HttpUtil.post('/panel/api/hostsfile/download', { url }, JSON_HEADERS),
    onSuccess: () => invalidate(),
  });

  return {
    save: (raw: string) => save.mutateAsync(raw),
    download: (url: string) => download.mutateAsync(url),
  };
}
