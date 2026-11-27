import { describe, expect, it } from 'vitest';
import { theme as antdTheme } from 'antd';

import { buildAntdThemeConfig, type ThemeName } from '@/hooks/useTheme';

interface Rgba {
  r: number;
  g: number;
  b: number;
  a: number;
}

function parseColor(raw: string): Rgba {
  const s = raw.trim();
  if (s.startsWith('#')) {
    let hex = s.slice(1);
    if (hex.length === 3) {
      hex = hex
        .split('')
        .map((c) => c + c)
        .join('');
    }
    return {
      r: parseInt(hex.slice(0, 2), 16),
      g: parseInt(hex.slice(2, 4), 16),
      b: parseInt(hex.slice(4, 6), 16),
      a: 1,
    };
  }
  const match = s.match(/rgba?\(([^)]+)\)/);
  if (!match) {
    throw new Error(`Unsupported color format in theme token: ${raw}`);
  }
  const [r, g, b, a = 1] = match[1].split(',').map((part) => Number(part.trim()));
  return { r, g, b, a };
}

function composite(fgRaw: string, bgRaw: string): [number, number, number] {
  const bg = parseColor(bgRaw);
  const fg = parseColor(fgRaw);
  const alpha = fg.a;
  return [
    fg.r * alpha + bg.r * (1 - alpha),
    fg.g * alpha + bg.g * (1 - alpha),
    fg.b * alpha + bg.b * (1 - alpha),
  ];
}

function luminance([r, g, b]: [number, number, number]): number {
  const linear = (channel: number) => {
    const v = channel / 255;
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(b);
}

function contrastRatio(fgRaw: string, bgRaw: string): number {
  const bgParsed = parseColor(bgRaw);
  const bgRgb: [number, number, number] =
    bgParsed.a < 1 ? composite(bgRaw, '#ffffff') : [bgParsed.r, bgParsed.g, bgParsed.b];
  const bgHex = `rgb(${bgRgb.join(',')})`;
  const fgRgb = composite(fgRaw, bgHex);
  const l1 = luminance(fgRgb);
  const l2 = luminance(bgRgb);
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
}

const CUSTOM_CASES: readonly { label: string; background: string; accent: string }[] = [
  { label: 'slate-sky', background: '#111827', accent: '#38bdf8' },
  { label: 'obsidian-violet', background: '#1a1b1f', accent: '#7c5cff' },
  { label: 'forest-mint', background: '#0f1f1a', accent: '#34d399' },
  { label: 'paper-amber', background: '#f6f3ec', accent: '#b45309' },
  { label: 'ice-cobalt', background: '#eef3f8', accent: '#2563eb' },
  { label: 'rose-crimson', background: '#fdf2f4', accent: '#be123c' },
  // Edge cases matching real contrast failures on custom themes.
  { label: 'light-black-accent', background: '#f3efe6', accent: '#141414' },
  { label: 'light-yellow-accent', background: '#ffffff', accent: '#fadb14' },
  { label: 'light-same-accent', background: '#ffffff', accent: '#ffffff' },
  { label: 'dark-black-accent', background: '#1a1b1f', accent: '#141414' },
  { label: 'dark-white-accent', background: '#000000', accent: '#ffffff' },
];

describe('theme token WCAG contrast invariants', () => {
  it.each<ThemeName>(['light', 'dark', 'ultra', 'lane'])(
    'maintains readable selected, hover, link and solid button contrast for %s theme',
    (name) => {
      const cfg = buildAntdThemeConfig(name);
      const token = antdTheme.getDesignToken(cfg);
      const siderBg =
        (cfg.components?.Layout as { siderBg?: string } | undefined)?.siderBg ??
        token.colorBgLayout;

      expect(contrastRatio(token.colorText, token.colorBgContainer)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorText, token.colorBgElevated)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorText, token.colorPrimaryBg)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorText, token.colorPrimaryBgHover)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorText, token.controlItemBgActive)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorText, token.controlItemBgActiveHover)).toBeGreaterThanOrEqual(
        4.5,
      );

      for (const surface of [
        token.colorBgContainer,
        token.colorBgElevated,
        token.colorPrimaryBg,
        token.colorPrimaryBgHover,
        siderBg,
      ]) {
        expect(contrastRatio(token.colorPrimaryText, surface)).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(token.colorPrimaryTextHover, surface)).toBeGreaterThanOrEqual(4.5);
      }

      expect(contrastRatio(token.colorLink, token.colorBgContainer)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorLinkHover, token.colorBgContainer)).toBeGreaterThanOrEqual(
        4.5,
      );
      expect(contrastRatio(token.colorInfoText, token.colorInfoBg)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorErrorText, token.colorBgContainer)).toBeGreaterThanOrEqual(
        4.5,
      );

      const tabsTokens = cfg.components?.Tabs as
        | { itemSelectedColor?: string; itemHoverColor?: string }
        | undefined;
      const paginationTokens = cfg.components?.Pagination as
        | { itemActiveColor?: string }
        | undefined;
      const buttonTokens = cfg.components?.Button as
        | { colorPrimary?: string; colorPrimaryHover?: string }
        | undefined;

      expect(
        contrastRatio(tabsTokens?.itemSelectedColor ?? token.colorPrimary, token.colorBgContainer),
      ).toBeGreaterThanOrEqual(4.5);
      expect(
        contrastRatio(
          tabsTokens?.itemHoverColor ?? token.colorPrimaryHover,
          token.colorBgContainer,
        ),
      ).toBeGreaterThanOrEqual(4.5);
      expect(
        contrastRatio(
          paginationTokens?.itemActiveColor ?? token.colorPrimary,
          token.colorBgContainer,
        ),
      ).toBeGreaterThanOrEqual(4.5);

      const solidPrimary = buttonTokens?.colorPrimary ?? token.colorPrimary;
      const solidPrimaryHover = buttonTokens?.colorPrimaryHover ?? token.colorPrimaryHover;
      expect(contrastRatio(token.colorTextLightSolid, solidPrimary)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorTextLightSolid, solidPrimaryHover)).toBeGreaterThanOrEqual(
        4.5,
      );
    },
  );

  it.each(CUSTOM_CASES)(
    'maintains readable tokens for custom theme ($label)',
    ({ background, accent }) => {
      const cfg = buildAntdThemeConfig('custom', accent, background);
      const token = antdTheme.getDesignToken(cfg);
      const siderBg =
        (cfg.components?.Layout as { siderBg?: string } | undefined)?.siderBg ??
        token.colorBgLayout;

      expect(contrastRatio(token.colorText, token.colorBgContainer)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorText, token.colorBgElevated)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorText, token.colorPrimaryBg)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorText, token.controlItemBgActive)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorText, token.controlItemBgActiveHover)).toBeGreaterThanOrEqual(
        4.5,
      );
      expect(contrastRatio(token.colorBgContainer, token.controlItemBgActive)).toBeGreaterThan(
        1.05,
      );

      for (const surface of [
        token.colorBgContainer,
        token.colorBgElevated,
        token.colorPrimaryBg,
        token.colorPrimaryBgHover,
        siderBg,
      ]) {
        expect(contrastRatio(token.colorPrimaryText, surface)).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(token.colorPrimaryTextHover, surface)).toBeGreaterThanOrEqual(4.5);
      }

      expect(contrastRatio(token.colorLink, token.colorBgContainer)).toBeGreaterThanOrEqual(4.5);
      expect(contrastRatio(token.colorLinkHover, token.colorBgContainer)).toBeGreaterThanOrEqual(
        4.5,
      );
      expect(contrastRatio(token.colorTextLightSolid, token.colorPrimary)).toBeGreaterThanOrEqual(
        4.0,
      );
      expect(
        contrastRatio(token.colorTextLightSolid, token.colorPrimaryHover),
      ).toBeGreaterThanOrEqual(4.0);
    },
  );
});
