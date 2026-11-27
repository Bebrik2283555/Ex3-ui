import { describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom/vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ConfigProvider } from 'antd';

import InboundFormModal from '@/pages/inbounds/form/InboundFormModal';
import { DBInbound } from '@/models/dbinbound';
import { HttpUtil } from '@/utils';

import { renderWithProviders } from './test-utils';

function renderInbound(mode: 'add' | 'edit' = 'add') {
  const inbound = new DBInbound({
    id: 41,
    protocol: 'vless',
    enable: true,
    port: 14443,
    settings: { clients: [], decryption: 'none', encryption: 'none' },
    streamSettings: { network: 'tcp', security: 'none', tcpSettings: {} },
  });
  renderWithProviders(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <InboundFormModal
        open
        mode={mode}
        dbInbound={mode === 'edit' ? inbound : null}
        dbInbounds={[]}
        availableNodes={[]}
        onClose={() => {}}
        onSaved={() => {}}
      />
    </ConfigProvider>,
  );
  return userEvent.setup();
}

function selectText(combobox: HTMLElement): string {
  return (combobox.closest('.ant-select')?.textContent ?? '').trim();
}

async function selectAntdOption(
  user: ReturnType<typeof userEvent.setup>,
  dialog: HTMLElement,
  combobox: HTMLElement,
  optionName: string,
) {
  await user.click(combobox);
  const dropdown = Array.from(
    dialog.querySelectorAll<HTMLElement>('.ant-select-dropdown:not(.ant-select-dropdown-hidden)'),
  ).at(-1);
  if (!dropdown) throw new Error(`Open select dropdown not found for '${combobox.id}'`);
  const option = Array.from(dropdown.querySelectorAll<HTMLElement>('.ant-select-item-option')).find(
    (el) => (el.getAttribute('title') ?? el.textContent ?? '').trim() === optionName,
  );
  if (!option) throw new Error(`Option '${optionName}' not found`);
  await user.click(option);
}

describe('Inbound screen-reader interaction', () => {
  it('exposes the selected protocol as the closed control value', () => {
    renderInbound();
    const protocol = screen.getByRole('combobox', { name: 'Protocol' });
    expect(selectText(protocol)).toBe('vless');
  });

  it('exposes all visible protocol options as selectable items within its dialog', async () => {
    const user = renderInbound();
    const dialog = screen.getByRole('dialog', { name: /^Add Inbound/ });
    const protocol = within(dialog).getByRole('combobox', { name: 'Protocol' });
    await user.click(protocol);
    expect(within(dialog).getByRole('option', { name: 'tuic' })).toBeInTheDocument();
    expect(
      within(dialog).getByRole('option', { name: 'vless', selected: true }),
    ).toBeInTheDocument();
    await user.click(within(dialog).getByRole('option', { name: 'shadowsocks' }));
    expect(selectText(protocol)).toBe('shadowsocks');
    await user.click(screen.getByRole('tab', { name: 'Protocol' }));
    expect(screen.getByRole('combobox', { name: 'Encryption method' })).toBeVisible();
  });

  it('selects transport by its option role and submits the actual create payload', async () => {
    const post = vi.mocked(HttpUtil.post).mockClear();
    const user = renderInbound();
    const dialog = screen.getByRole('dialog', { name: /^Add Inbound/ });
    await user.click(screen.getByRole('tab', { name: 'Stream' }));
    const transport = screen.getByRole('combobox', { name: 'Transmission' });
    await selectAntdOption(user, dialog, transport, 'WebSocket');
    const path = screen.getByRole('textbox', { name: 'Path' });
    await user.clear(path);
    await user.type(path, '/accessible');
    await user.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/panel/api/inbounds/add', expect.anything()),
    );
    const payload = post.mock.calls.find(
      ([url]) => url === '/panel/api/inbounds/add',
    )![1] as Record<string, unknown>;
    expect(JSON.parse(String(payload.streamSettings))).toMatchObject({
      network: 'ws',
      security: 'none',
      wsSettings: { path: '/accessible' },
    });
  });

  it('preserves the disabled protocol in edit mode and exposes a named security group', async () => {
    const user = renderInbound('edit');
    expect(screen.getByRole('combobox', { name: 'Protocol' })).toBeDisabled();
    await user.click(screen.getByRole('tab', { name: 'Security' }));
    const security = screen.getByRole('radiogroup', { name: 'Security' });
    expect(
      within(security)
        .getAllByRole('radio')
        .map((radio) => (radio as HTMLInputElement).value),
    ).toEqual(['none', 'tls', 'reality']);
  });

  it('preserves edit serialization after a keyboard-accessible transport change', async () => {
    const post = vi.mocked(HttpUtil.post).mockClear();
    const user = renderInbound('edit');
    const dialog = screen.getByRole('dialog', { name: 'Modify Inbound' });
    await user.click(screen.getByRole('tab', { name: 'Stream' }));
    const transport = screen.getByRole('combobox', { name: 'Transmission' });
    await selectAntdOption(user, dialog, transport, 'gRPC');
    await user.click(screen.getByRole('button', { name: 'Save Changes' }));
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/panel/api/inbounds/update/41', expect.anything()),
    );
    const payload = post.mock.calls.find(
      ([url]) => url === '/panel/api/inbounds/update/41',
    )![1] as Record<string, unknown>;
    expect(payload).toMatchObject({ protocol: 'vless', port: 14443, enable: true });
    expect(JSON.parse(String(payload.streamSettings))).toMatchObject({
      network: 'grpc',
      security: 'none',
      grpcSettings: { serviceName: '' },
    });
    expect(JSON.parse(String(payload.settings))).not.toHaveProperty('clients');
  });

  it('reveals and describes a validation error on a hidden tab without submitting', async () => {
    const post = vi.mocked(HttpUtil.post).mockClear();
    const user = renderInbound();
    const dialog = screen.getByRole('dialog', { name: /^Add Inbound/ });
    const strategy = screen.getByRole('combobox', { name: 'Share address strategy' });
    await selectAntdOption(user, dialog, strategy, 'Custom');
    const address = screen.getByRole('textbox', { name: 'Custom share address' });
    await user.type(address, 'https://example.test/path');
    await user.click(screen.getByRole('tab', { name: 'Stream' }));
    await user.click(screen.getByRole('button', { name: 'Create' }));
    await waitFor(() => expect(address).toBeVisible());
    expect(address).toHaveAttribute('aria-invalid', 'true');
    expect(address).toHaveAccessibleDescription(expect.stringContaining('without a scheme'));
    await waitFor(() => expect(address).toHaveFocus());
    expect(post.mock.calls.filter(([url]) => url === '/panel/api/inbounds/add')).toEqual([]);
  });
});
