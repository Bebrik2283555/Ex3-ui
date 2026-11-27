import { Form } from 'antd';
import { QueryClientProvider } from '@tanstack/react-query';
import { act, render, waitFor } from '@testing-library/react';
import { FormProvider, useForm } from 'react-hook-form';
import { describe, expect, it } from 'vitest';

import { AntdRootConfig } from '@/i18n/AntdRootConfig';
import { ThemeProvider } from '@/hooks/useTheme';
import { createDefaultOlcrtcInboundSettings } from '@/lib/xray/inbound-defaults';
import SidecarFields from '@/pages/inbounds/form/protocols/sidecar';
import type { InboundFormValues } from '@/schemas/forms/inbound-form';
import {
  OlcrtcInboundSettingsSchema,
  OlcrtcSettingsSchema,
} from '@/schemas/protocols/inbound/sidecar';
import { makeTestQueryClient } from './test-utils';

function Harness({
  provider,
  transport = 'datachannel',
  authToken = '',
  iceLocalIp = '',
  onReady,
}: {
  provider: string;
  transport?: string;
  authToken?: string;
  iceLocalIp?: string;
  onReady?: (methods: ReturnType<typeof useForm<InboundFormValues>>) => void;
}) {
  const defaults = createDefaultOlcrtcInboundSettings();
  const methods = useForm<InboundFormValues>({
    defaultValues: {
      protocol: 'olcrtc',
      settings: {
        ...defaults,
        sidecar: {
          ...defaults.sidecar,
          provider: provider as never,
          transport: transport as never,
          authToken,
          iceLocalIp,
        },
      },
    } as unknown as InboundFormValues,
  });
  onReady?.(methods);
  return (
    <FormProvider {...methods}>
      <Form>
        <SidecarFields />
      </Form>
    </FormProvider>
  );
}

describe('Ghostlane olcRTC schema and defaults', () => {
  it('accepts all Ghostlane providers and DTLS/ICE/UDP fields', () => {
    const defaults = createDefaultOlcrtcInboundSettings();
    expect(OlcrtcInboundSettingsSchema.parse(defaults).sidecar.dtlsProfile).toBe('');

    for (const provider of [
      'jitsi',
      'telemost',
      'wbstream',
      'salutejazz',
      'vkcalls',
      'mtslink',
      'max',
    ] as const) {
      const parsed = OlcrtcSettingsSchema.parse({
        provider,
        authToken: provider === 'max' ? 'sess-key' : '',
        roomId: 'room-1',
        iceLocalIp: '198.51.100.2',
        dtlsProfile: 'chrome-linux-138-compat-v1',
        udpEnabled: true,
      });
      expect(parsed.provider).toBe(provider);
      expect(parsed.dtlsProfile).toBe('chrome-linux-138-compat-v1');
      expect(parsed.udpEnabled).toBe(true);
    }

    expect(OlcrtcSettingsSchema.safeParse({ provider: 'unknown-carrier' }).success).toBe(false);
    expect(OlcrtcSettingsSchema.safeParse({ dtlsProfile: 'unknown-dtls' }).success).toBe(false);
  });

  it('switches transport to supported fallback on provider or token change', async () => {
    const queryClient = makeTestQueryClient();
    const Wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>
        <AntdRootConfig>
          <ThemeProvider>{children}</ThemeProvider>
        </AntdRootConfig>
      </QueryClientProvider>
    );
    let formMethods: ReturnType<typeof useForm<InboundFormValues>> | undefined;
    const view = render(
      <Harness
        provider="vkcalls"
        transport="datachannel"
        onReady={(m) => {
          formMethods = m;
        }}
      />,
      { wrapper: Wrapper },
    );

    await waitFor(() => {
      expect(formMethods?.getValues('settings.sidecar.transport' as never)).toBe('vp8channel');
    });
    expect(view.queryByLabelText('Auth / account token')).toBeNull();

    await act(async () => {
      formMethods?.setValue('settings.sidecar.provider' as never, 'wbstream' as never);
      formMethods?.setValue('settings.sidecar.transport' as never, 'datachannel' as never);
    });
    expect(view.getByLabelText('Auth / account token')).toBeTruthy();
    await waitFor(() => {
      expect(formMethods?.getValues('settings.sidecar.transport' as never)).toBe('vp8channel');
    });

    await act(async () => {
      formMethods?.setValue('settings.sidecar.iceLocalIp' as never, '127.0.0.1' as never);
      await formMethods?.trigger('settings.sidecar.iceLocalIp' as never);
    });
    expect(view.getByText('Enter a valid unicast IPv4 address')).toBeTruthy();
  });
});
