import { createContext, useCallback, useContext, useLayoutEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { theme as antdTheme } from 'antd';
import type { ThemeConfig } from 'antd';

const STORAGE_THEME = 'xui-theme-name';
const STORAGE_ACCENT = 'xui-theme-accent';
const STORAGE_CUSTOM_BG = 'xui-theme-custom-bg';
const STORAGE_DARK = 'dark-mode';
const STORAGE_ULTRA = 'isUltraDarkThemeEnabled';

export type ThemeName = 'light' | 'dark' | 'ultra' | 'lane' | 'custom';

export const DEFAULT_ACCENT = '#1677ff';
export const DEFAULT_CUSTOM_BG = '#1a1b1f';

function readBool(key: string, fallback: boolean): boolean {
  const raw = localStorage.getItem(key);
  if (raw === null) return fallback;
  return raw === 'true';
}

function readThemeName(): ThemeName {
  const raw = localStorage.getItem(STORAGE_THEME);
  if (raw === 'light' || raw === 'dark' || raw === 'ultra' || raw === 'lane' || raw === 'custom') {
    return raw;
  }
  // Panels upgraded from the two-switch theme: keep the same look.
  if (readBool(STORAGE_ULTRA, false)) return 'ultra';
  return readBool(STORAGE_DARK, true) ? 'dark' : 'light';
}

function readAccent(): string {
  const raw = localStorage.getItem(STORAGE_ACCENT);
  return raw && /^#[0-9a-f]{6}$/i.test(raw) ? raw : DEFAULT_ACCENT;
}

function readCustomBg(): string {
  const raw = localStorage.getItem(STORAGE_CUSTOM_BG);
  return raw && /^#[0-9a-f]{6}$/i.test(raw) ? raw : DEFAULT_CUSTOM_BG;
}

function clampChannel(value: number): number {
  return Math.min(255, Math.max(0, Math.round(value)));
}

function hexToRgb(hex: string): [number, number, number] {
  const value = hex.replace('#', '');
  return [
    parseInt(value.slice(0, 2), 16),
    parseInt(value.slice(2, 4), 16),
    parseInt(value.slice(4, 6), 16),
  ];
}

function rgbToHex(red: number, green: number, blue: number): string {
  return `#${[red, green, blue]
    .map((channel) => clampChannel(channel).toString(16).padStart(2, '0'))
    .join('')}`;
}

function mixColors(from: string, to: string, weight: number): string {
  const [fr, fg, fb] = hexToRgb(from);
  const [tr, tg, tb] = hexToRgb(to);
  return rgbToHex(fr + (tr - fr) * weight, fg + (tg - fg) * weight, fb + (tb - fb) * weight);
}

function lighten(color: string, amount: number): string {
  return mixColors(color, '#ffffff', amount);
}

function darken(color: string, amount: number): string {
  return mixColors(color, '#000000', amount);
}

function linearize(channel: number): number {
  const value = channel / 255;
  return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
}

function relativeLuminance(hex: string): number {
  const [red, green, blue] = hexToRgb(hex);
  return 0.2126 * linearize(red) + 0.7152 * linearize(green) + 0.0722 * linearize(blue);
}

export function isLightColor(hex: string): boolean {
  return relativeLuminance(hex) > 0.45;
}

function contrastText(hex: string): string {
  return isLightColor(hex) ? '#141414' : '#ffffff';
}

export function isDarkTheme(name: ThemeName, customBg: string = DEFAULT_CUSTOM_BG): boolean {
  if (name === 'custom') {
    return !isLightColor(customBg);
  }
  return name !== 'light' && name !== 'lane';
}

function applyDom(name: ThemeName, customBg: string = DEFAULT_CUSTOM_BG) {
  const isDark = isDarkTheme(name, customBg);
  const isUltra = name === 'ultra';
  document.body.classList.remove('dark', 'light', 'theme-dark', 'theme-light', 'theme-ultra');
  document.body.classList.remove('theme-lane', 'theme-custom');
  document.body.classList.add(isDark ? 'dark' : 'light');
  document.body.classList.add(`theme-${name}`);
  // Native scrollbars read color-scheme, not the body class.
  document.documentElement.style.colorScheme = isDark ? 'dark' : 'light';
  if (isUltra) {
    document.documentElement.setAttribute('data-theme', 'ultra-dark');
  } else if (name === 'lane') {
    document.documentElement.setAttribute('data-theme', 'lane');
  } else if (name === 'custom') {
    document.documentElement.setAttribute('data-theme', 'custom');
  } else {
    document.documentElement.removeAttribute('data-theme');
  }
  const msg = document.getElementById('message');
  if (msg) {
    msg.classList.remove('dark', 'light');
    msg.classList.add(isDark ? 'dark' : 'light');
  }
}

// module load so the document is in the right theme before React mounts.
const initialTheme = readThemeName();
const initialAccentValue = readAccent();
const initialCustomBg = readCustomBg();
applyDom(initialTheme, initialCustomBg);

const DARK_TOKENS = {
  colorBgBase: '#1a1b1f',
  colorBgLayout: '#1a1b1f',
  colorBgContainer: '#23252b',
  colorBgElevated: '#2d2f37',
};
const ULTRA_DARK_TOKENS = {
  colorBgBase: '#000',
  colorBgLayout: '#000',
  colorBgContainer: '#101013',
  colorBgElevated: '#1a1a1e',
};
const DARK_LAYOUT_TOKENS = {
  bodyBg: '#1a1b1f',
  headerBg: '#15161a',
  headerColor: '#ffffff',
  footerBg: '#1a1b1f',
  siderBg: '#15161a',
  triggerBg: '#23252b',
  triggerColor: '#ffffff',
};
const ULTRA_DARK_LAYOUT_TOKENS = {
  bodyBg: '#000',
  headerBg: '#050507',
  headerColor: '#ffffff',
  footerBg: '#000',
  siderBg: '#050507',
  triggerBg: '#1a1a1e',
  triggerColor: '#ffffff',
};
const DARK_MENU_TOKENS = {
  darkItemBg: '#15161a',
  darkSubMenuItemBg: '#1a1b1f',
  darkPopupBg: '#23252b',
};
const ULTRA_DARK_MENU_TOKENS = {
  darkItemBg: '#050507',
  darkSubMenuItemBg: '#000',
  darkPopupBg: '#101013',
};
const DARK_CARD_TOKENS = {
  colorBorderSecondary: 'rgba(255, 255, 255, 0.06)',
};
const ULTRA_DARK_CARD_TOKENS = {
  colorBorderSecondary: 'rgba(255, 255, 255, 0.04)',
};

// Lane: warm paper, near-black type and one dark accent. The reference is the
// studio site sklane.com — lots of air, soft cards and no glass effects.
const LANE_TOKENS = {
  colorPrimary: '#141414',
  colorInfo: '#141414',
  colorLink: '#141414',
  colorText: '#141414',
  colorTextSecondary: 'rgba(20, 20, 20, 0.68)',
  colorTextTertiary: 'rgba(20, 20, 20, 0.52)',
  colorBgBase: '#f3efe6',
  colorBgLayout: '#f3efe6',
  colorBgContainer: '#fbf8f1',
  colorBgElevated: '#fffdf8',
  colorBorder: 'rgba(20, 20, 20, 0.16)',
  colorBorderSecondary: 'rgba(20, 20, 20, 0.09)',
  borderRadius: 14,
  borderRadiusLG: 20,
  fontFamily:
    "'Inter', 'Segoe UI', -apple-system, BlinkMacSystemFont, 'Helvetica Neue', Arial, sans-serif",
};
const LANE_LAYOUT_TOKENS = {
  bodyBg: '#f3efe6',
  headerBg: '#f3efe6',
  headerColor: '#141414',
  footerBg: '#f3efe6',
  siderBg: '#efeade',
  triggerBg: '#141414',
  triggerColor: '#fbf8f1',
};
const LANE_MENU_TOKENS = {
  itemBg: 'transparent',
  itemColor: 'rgba(20, 20, 20, 0.78)',
  itemSelectedBg: '#141414',
  itemSelectedColor: '#fbf8f1',
  itemHoverBg: 'rgba(20, 20, 20, 0.06)',
  subMenuItemBg: 'transparent',
};
const LANE_CARD_TOKENS = {
  colorBorderSecondary: 'rgba(20, 20, 20, 0.08)',
};

const STATISTIC_TOKENS = {
  contentFontSize: 17,
  titleFontSize: 11,
};
const LIGHT_CONTRAST_TOKENS = {
  colorTextDescription: 'rgba(0, 0, 0, 0.58)',
  colorTextTertiary: 'rgba(0, 0, 0, 0.58)',
  colorTextPlaceholder: '#767676',
  colorError: '#cf1322',
  colorErrorText: '#cf1322',
  colorSuccessText: '#237804',
};
const LIGHT_BUTTON_TOKENS = {
  colorPrimary: '#0958d9',
  colorPrimaryHover: '#2468e5',
  colorPrimaryActive: '#073ea8',
};

// The custom theme derives every surface from one background colour, then uses
// the second colour only for interactive accents. Text automatically flips to
// the readable side of the background, so light and dark bases both work.
function buildCustomPalette(background: string, accent: string) {
  const baseLuminance = relativeLuminance(background);
  const light = baseLuminance > 0.45;
  const text = contrastText(background);
  const textSecondary = light ? 'rgba(20, 20, 20, 0.68)' : 'rgba(255, 255, 255, 0.68)';
  const textTertiary = light ? 'rgba(20, 20, 20, 0.52)' : 'rgba(255, 255, 255, 0.52)';
  const textPlaceholder = light ? 'rgba(20, 20, 20, 0.42)' : 'rgba(255, 255, 255, 0.42)';
  const border = light ? 'rgba(20, 20, 20, 0.16)' : 'rgba(255, 255, 255, 0.14)';
  const borderSecondary = light ? 'rgba(20, 20, 20, 0.09)' : 'rgba(255, 255, 255, 0.06)';
  const layout = light ? darken(background, 0.025) : background;
  // Mid-tone bases need a larger lift so cards stay distinguishable from the page.
  const surfaceBoost = baseLuminance < 0.03 ? 0.06 : 0.09;
  const container = light
    ? baseLuminance > 0.9
      ? background
      : lighten(background, 0.35)
    : lighten(background, surfaceBoost);
  const elevated = light ? lighten(background, 0.7) : lighten(background, surfaceBoost + 0.06);
  const frameShade = surfaceBoost > 0.06 ? 0.07 : 0.04;
  const header = light ? background : darken(background, frameShade);
  const sider = light ? darken(background, 0.035) : darken(background, frameShade);
  const triggerBg = light ? darken(background, 0.75) : lighten(background, 0.12);
  const triggerColor = light ? container : text;

  return {
    light,
    tokens: {
      colorPrimary: accent,
      colorInfo: accent,
      colorLink: accent,
      colorText: text,
      colorTextSecondary: textSecondary,
      colorTextTertiary: textTertiary,
      colorTextPlaceholder: textPlaceholder,
      colorBgBase: background,
      colorBgLayout: layout,
      colorBgContainer: container,
      colorBgElevated: elevated,
      colorBorder: border,
      colorBorderSecondary: borderSecondary,
    },
    layout: {
      bodyBg: layout,
      headerBg: header,
      headerColor: text,
      footerBg: layout,
      siderBg: sider,
      triggerBg,
      triggerColor,
    },
    menu: light
      ? {
          itemBg: 'transparent',
          itemColor: textSecondary,
          itemSelectedBg: accent,
          itemSelectedColor: contrastText(accent),
          itemHoverBg: 'rgba(20, 20, 20, 0.06)',
          subMenuItemBg: 'transparent',
        }
      : {
          darkItemBg: header,
          darkSubMenuItemBg: layout,
          darkPopupBg: container,
          darkItemColor: textSecondary,
          darkItemHoverColor: text,
          darkItemHoverBg: 'rgba(255, 255, 255, 0.08)',
          darkItemSelectedBg: accent,
          darkItemSelectedColor: contrastText(accent),
        },
    card: { colorBorderSecondary: borderSecondary },
  };
}

// hashed:false drops the `:where(.css-<hash>)` wrapper antd puts around every
// rule. It costs nothing in specificity — `:where()` contributes zero, so the
// panel's own `.ant-*` overrides still win — and it removes roughly 5,700
// wrappers, 16% of the generated stylesheet, from what the browser has to parse.
//
// cssVar.key pins the CSS-variable scope. Every panel page mounts its own
// ConfigProvider (there is no root one), and without a fixed key each mints a
// fresh useId-derived scope, so navigating re-serialises and re-injects the whole
// token block under a new class instead of reusing the one already in the head.
const SHARED_STYLE_CONFIG = {
  hashed: false,
  cssVar: { key: 'xui' },
} as const;

export function buildAntdThemeConfig(
  name: ThemeName = initialTheme,
  accent: string = initialAccentValue,
  customBg: string = initialCustomBg,
): ThemeConfig {
  if (name === 'lane') {
    return {
      ...SHARED_STYLE_CONFIG,
      algorithm: antdTheme.defaultAlgorithm,
      token: LANE_TOKENS,
      components: {
        Layout: LANE_LAYOUT_TOKENS,
        Menu: LANE_MENU_TOKENS,
        Card: LANE_CARD_TOKENS,
        Statistic: STATISTIC_TOKENS,
      },
    };
  }
  if (name === 'light') {
    return {
      ...SHARED_STYLE_CONFIG,
      algorithm: antdTheme.defaultAlgorithm,
      token: LIGHT_CONTRAST_TOKENS,
      components: {
        Statistic: STATISTIC_TOKENS,
        Button: LIGHT_BUTTON_TOKENS,
      },
    };
  }
  if (name === 'custom') {
    const palette = buildCustomPalette(customBg, accent);
    return {
      ...SHARED_STYLE_CONFIG,
      algorithm: palette.light ? antdTheme.defaultAlgorithm : antdTheme.darkAlgorithm,
      token: palette.tokens,
      components: {
        Layout: palette.layout,
        Menu: palette.menu,
        Card: palette.card,
        Statistic: STATISTIC_TOKENS,
      },
    };
  }
  const isUltra = name === 'ultra';
  return {
    ...SHARED_STYLE_CONFIG,
    algorithm: antdTheme.darkAlgorithm,
    token: isUltra ? ULTRA_DARK_TOKENS : DARK_TOKENS,
    components: {
      Layout: isUltra ? ULTRA_DARK_LAYOUT_TOKENS : DARK_LAYOUT_TOKENS,
      Menu: isUltra ? ULTRA_DARK_MENU_TOKENS : DARK_MENU_TOKENS,
      Card: isUltra ? ULTRA_DARK_CARD_TOKENS : DARK_CARD_TOKENS,
      Statistic: STATISTIC_TOKENS,
    },
  };
}

export function pauseAnimationsUntilLeave(elementId: string): void {
  document.documentElement.setAttribute('data-theme-animations', 'off');
  const el = document.getElementById(elementId);
  if (!el) return;
  const restore = () => {
    document.documentElement.removeAttribute('data-theme-animations');
    el.removeEventListener('mouseleave', restore);
    el.removeEventListener('touchend', restore);
  };
  el.addEventListener('mouseleave', restore);
  el.addEventListener('touchend', restore);
}

interface ThemeContextValue {
  themeName: ThemeName;
  setThemeName: (name: ThemeName) => void;
  accent: string;
  setAccent: (color: string) => void;
  customBg: string;
  setCustomBg: (color: string) => void;
  isDark: boolean;
  isUltra: boolean;
  toggleTheme: () => void;
  toggleUltra: () => void;
  antdThemeConfig: ThemeConfig;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [themeName, setThemeNameState] = useState<ThemeName>(initialTheme);
  const [accent, setAccentState] = useState<string>(initialAccentValue);
  const [customBg, setCustomBgState] = useState<string>(initialCustomBg);

  const isDark = isDarkTheme(themeName, customBg);
  const isUltra = themeName === 'ultra';

  useLayoutEffect(() => {
    applyDom(themeName, customBg);
    localStorage.setItem(STORAGE_THEME, themeName);
    // Keep the legacy flags in sync: the subscription page and older builds
    // still read them.
    localStorage.setItem(STORAGE_DARK, String(isDark));
    localStorage.setItem(STORAGE_ULTRA, String(isUltra));
    localStorage.setItem(STORAGE_ACCENT, accent);
    localStorage.setItem(STORAGE_CUSTOM_BG, customBg);
  }, [themeName, customBg, isDark, isUltra, accent]);

  const setThemeName = useCallback((name: ThemeName) => setThemeNameState(name), []);
  const setAccent = useCallback((color: string) => {
    if (/^#[0-9a-f]{6}$/i.test(color)) setAccentState(color);
  }, []);
  const setCustomBg = useCallback((color: string) => {
    if (/^#[0-9a-f]{6}$/i.test(color)) setCustomBgState(color);
  }, []);
  const toggleTheme = useCallback(() => {
    setThemeNameState(isDark ? 'light' : 'dark');
  }, [isDark]);
  const toggleUltra = useCallback(() => {
    setThemeNameState((prev) => (prev === 'ultra' ? 'dark' : 'ultra'));
  }, []);

  const antdThemeConfig = useMemo(
    () => buildAntdThemeConfig(themeName, accent, customBg),
    [themeName, accent, customBg],
  );

  const value = useMemo<ThemeContextValue>(
    () => ({
      themeName,
      setThemeName,
      accent,
      setAccent,
      customBg,
      setCustomBg,
      isDark,
      isUltra,
      toggleTheme,
      toggleUltra,
      antdThemeConfig,
    }),
    [
      themeName,
      setThemeName,
      accent,
      setAccent,
      customBg,
      setCustomBg,
      isDark,
      isUltra,
      toggleTheme,
      toggleUltra,
      antdThemeConfig,
    ],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme must be used inside <ThemeProvider>');
  return ctx;
}
