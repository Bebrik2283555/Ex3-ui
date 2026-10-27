import { useTranslation } from 'react-i18next';
import { ColorPicker, Modal } from 'antd';
import { BgColorsOutlined, CheckOutlined } from '@ant-design/icons';

import {
  DEFAULT_ACCENT,
  DEFAULT_CUSTOM_BG,
  isLightColor,
  useTheme,
  type ThemeName,
} from '@/hooks/useTheme';
import './ThemePickerModal.css';

interface ThemePickerModalProps {
  open: boolean;
  onClose: () => void;
}

interface ThemeOption {
  name: ThemeName;
  labelKey: string;
  descKey?: string;
  background: string;
  foreground: string;
  accent: string;
}

const OPTIONS: ThemeOption[] = [
  {
    name: 'light',
    labelKey: 'theme.light',
    background: '#ffffff',
    foreground: '#141414',
    accent: '#0958d9',
  },
  {
    name: 'dark',
    labelKey: 'theme.dark',
    background: '#1a1b1f',
    foreground: '#f5f5f5',
    accent: '#1677ff',
  },
  {
    name: 'ultra',
    labelKey: 'theme.ultra',
    background: '#000000',
    foreground: '#f5f5f5',
    accent: '#1677ff',
  },
  {
    name: 'lane',
    labelKey: 'theme.lane',
    descKey: 'theme.laneDesc',
    background: '#f3efe6',
    foreground: '#141414',
    accent: '#141414',
  },
  {
    name: 'custom',
    labelKey: 'theme.custom',
    descKey: 'theme.customDesc',
    background: DEFAULT_CUSTOM_BG,
    foreground: '#f5f5f5',
    accent: '#7c5cff',
  },
];

const CUSTOM_PRESETS = [
  { background: '#111827', accent: '#38bdf8' },
  { background: '#1a1b1f', accent: '#7c5cff' },
  { background: '#0f1f1a', accent: '#34d399' },
  { background: '#f6f3ec', accent: '#b45309' },
  { background: '#eef3f8', accent: '#2563eb' },
  { background: '#fdf2f4', accent: '#be123c' },
];

const BACKGROUND_PRESETS = [
  '#ffffff',
  '#f6f3ec',
  '#eef3f8',
  '#fdf2f4',
  DEFAULT_CUSTOM_BG,
  '#111827',
  '#0f1f1a',
  '#000000',
];

export default function ThemePickerModal({ open, onClose }: ThemePickerModalProps) {
  const { t } = useTranslation();
  const { themeName, setThemeName, accent, setAccent, customBg, setCustomBg } = useTheme();

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={560}
      destroyOnHidden
      title={
        <>
          <BgColorsOutlined aria-hidden /> {t('menu.theme')}
        </>
      }
    >
      <div className="theme-picker-grid">
        {OPTIONS.map((opt) => {
          const selected = themeName === opt.name;
          const isCustom = opt.name === 'custom';
          const previewBackground = isCustom ? customBg : opt.background;
          const previewForeground = isCustom
            ? isLightColor(customBg)
              ? '#141414'
              : '#f5f5f5'
            : opt.foreground;
          const previewAccent = isCustom ? accent : opt.accent;
          return (
            <button
              type="button"
              key={opt.name}
              className={`theme-picker-card${selected ? ' is-selected' : ''}`}
              onClick={() => setThemeName(opt.name)}
            >
              <span
                className="theme-picker-preview"
                style={{ background: previewBackground, color: previewForeground }}
              >
                <span className="theme-picker-preview-bar" style={{ background: previewAccent }} />
                <span className="theme-picker-preview-line" />
                <span className="theme-picker-preview-line short" />
                <span
                  className="theme-picker-preview-button"
                  style={{ background: previewAccent }}
                />
              </span>
              <span className="theme-picker-meta">
                <span className="theme-picker-name">
                  {t(opt.labelKey)}
                  {selected && <CheckOutlined aria-hidden className="theme-picker-check" />}
                </span>
                {opt.descKey && <span className="theme-picker-desc">{t(opt.descKey)}</span>}
              </span>
            </button>
          );
        })}
      </div>

      {themeName === 'custom' && (
        <div className="theme-picker-custom">
          <span className="theme-picker-presets-label">{t('theme.recommended')}</span>
          <div className="theme-picker-presets" role="group" aria-label={t('theme.recommended')}>
            {CUSTOM_PRESETS.map((preset) => {
              const selected = customBg === preset.background && accent === preset.accent;
              return (
                <button
                  type="button"
                  key={`${preset.background}-${preset.accent}`}
                  className={`theme-picker-preset${selected ? ' is-selected' : ''}`}
                  onClick={() => {
                    setCustomBg(preset.background);
                    setAccent(preset.accent);
                  }}
                  title={`${preset.background} / ${preset.accent}`}
                  aria-label={`${t('theme.recommended')}: ${preset.background} / ${preset.accent}`}
                >
                  <span
                    className="theme-picker-preset-swatch"
                    style={{ background: preset.background }}
                  >
                    <span
                      className="theme-picker-preset-accent"
                      style={{ background: preset.accent }}
                    />
                  </span>
                </button>
              );
            })}
          </div>
          <div className="theme-picker-color-row">
            <span>{t('theme.background')}</span>
            <ColorPicker
              value={customBg}
              disabledAlpha
              onChange={(color) => setCustomBg(color.toHexString())}
              presets={[{ label: t('theme.background'), colors: BACKGROUND_PRESETS }]}
            />
          </div>
          <div className="theme-picker-color-row">
            <span>{t('theme.accent')}</span>
            <ColorPicker
              value={accent}
              disabledAlpha
              onChange={(color) => setAccent(color.toHexString())}
              presets={[
                {
                  label: t('theme.accent'),
                  colors: [DEFAULT_ACCENT, '#7c5cff', '#ec4899', '#10b981', '#f59e0b', '#ef4444'],
                },
              ]}
            />
          </div>
        </div>
      )}
    </Modal>
  );
}
