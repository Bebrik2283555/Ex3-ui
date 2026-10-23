import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';

export interface CoreInfo {
  name: string;
  displayName: string;
  installed: boolean;
  enabled: boolean;
  running: boolean;
  inbounds: number;
  runningInbounds: number;
  binary: string;
  size: number;
  modified: number;
  lastError?: string;
}

export interface CoreInboundInfo {
  id: number;
  remark: string;
  protocol: string;
  listen?: string;
  port: number;
  configDir?: string;
  running: boolean;
  lastError?: string;
  logs?: string[];
  clients: number;
  links?: string[];
  legacyUri?: string;
  subJson?: string;
}

export interface CoreDetails extends CoreInfo {
  inboundList: CoreInboundInfo[];
}

const CORE_KEYS = ['cores'] as const;

export function useCoresQuery() {
  return useQuery({
    queryKey: CORE_KEYS,
    queryFn: async (): Promise<CoreInfo[]> => {
      const msg = await HttpUtil.get<CoreInfo[]>('/panel/api/cores', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to load cores');
      return msg.obj ?? [];
    },
    refetchInterval: 5000,
  });
}

export function useCoreDetailsQuery(name: string, enabled: boolean) {
  return useQuery({
    queryKey: [...CORE_KEYS, name, 'details'],
    enabled,
    queryFn: async (): Promise<CoreDetails> => {
      const msg = await HttpUtil.get<CoreDetails>(`/panel/api/cores/${name}/details`, undefined, {
        silent: true,
      });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to load core details');
      return msg.obj as CoreDetails;
    },
    refetchInterval: enabled ? 5000 : false,
  });
}

export function useCoreActions() {
  const queryClient = useQueryClient();
  const invalidate = () => queryClient.invalidateQueries({ queryKey: CORE_KEYS });

  const start = useMutation({
    mutationFn: (name: string) => HttpUtil.post(`/panel/api/cores/${name}/start`),
    onSuccess: invalidate,
  });
  const stop = useMutation({
    mutationFn: (name: string) => HttpUtil.post(`/panel/api/cores/${name}/stop`),
    onSuccess: invalidate,
  });
  const restart = useMutation({
    mutationFn: (name: string) => HttpUtil.post(`/panel/api/cores/${name}/restart`),
    onSuccess: invalidate,
  });

  return { start, stop, restart };
}
