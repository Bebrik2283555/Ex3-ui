import { useCallback, useEffect, useMemo, useState } from 'react';
import type { CSSProperties } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  ConfigProvider,
  Form,
  Input,
  Layout,
  Menu,
  Popover,
  Space,
  Spin,
  message,
} from 'antd';
import {
  KeyOutlined,
  LockOutlined,
  MoonFilled,
  MoonOutlined,
  SunOutlined,
  TranslationOutlined,
  UserOutlined,
} from '@ant-design/icons';

import { FormProvider, useForm } from 'react-hook-form';
import { HttpUtil, LanguageManager } from '@/utils';
import { FormField, rhfZodValidate } from '@/components/form/rhf';
import { setMessageInstance } from '@/utils/messageBus';
import ThemePickerModal from '@/components/ui/ThemePickerModal';
import { useTheme } from '@/hooks/useTheme';
import { LoginFormSchema, TwoFactorCodeSchema, type LoginFormValues } from '@/schemas/login';
import './LoginPage.css';

const HEADLINE_INTERVAL_MS = 2000;

type LoginForm = LoginFormValues;

const basePath = window.X_UI_BASE_PATH || '';

export default function LoginPage() {
  const { t } = useTranslation();
  const { isDark, isUltra, themeName, accent, customBg, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  const [themePickerOpen, setThemePickerOpen] = useState(false);

  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);

  const [fetched, setFetched] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [twoFactorEnable, setTwoFactorEnable] = useState(false);
  const [headlineIndex, setHeadlineIndex] = useState(0);
  const methods = useForm<LoginForm>({
    defaultValues: { username: '', password: '', twoFactorCode: '' },
  });
  const [lang, setLang] = useState<string>(() => LanguageManager.getLanguage());

  const headlineWords = useMemo(() => [t('pages.login.hello'), t('pages.login.title')], [t]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      setHeadlineIndex((i) => (i + 1) % headlineWords.length);
    }, HEADLINE_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [headlineWords.length]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      // Bound the 2FA probe: while the panel restarts (e.g. right after a DB
      // import) this request can hang indefinitely, leaving an eternal spinner
      // with no way forward. On timeout the form still renders.
      const msg = await HttpUtil.post('/getTwoFactorEnable', undefined, { timeout: 10_000 });
      if (cancelled) return;
      if (msg.success) setTwoFactorEnable(!!msg.obj);
      setFetched(true);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const onSubmit = useCallback(async (values: LoginForm) => {
    setSubmitting(true);
    try {
      const msg = await HttpUtil.post('/login', values);
      if (msg.success) window.location.href = basePath + 'panel/';
    } finally {
      setSubmitting(false);
    }
  }, []);

  const onLangChange = useCallback((next: string) => {
    setLang(next);
    LanguageManager.setLanguage(next);
  }, []);

  const pageClass = useMemo(() => {
    const classes = ['login-app'];
    if (isDark) classes.push('is-dark');
    if (isUltra) classes.push('is-ultra');
    // Lane and custom themes have no is-dark/is-ultra switch: they carry their
    // own surfaces, so the login page has to opt into them by name.
    if (themeName === 'lane') classes.push('is-lane');
    if (themeName === 'custom') classes.push('is-custom');
    return classes.join(' ');
  }, [isDark, isUltra, themeName]);

  const themeVars = useMemo<CSSProperties | undefined>(() => {
    if (themeName !== 'custom') return undefined;
    const token = antdThemeConfig.token ?? {};
    return {
      '--login-bg': String(token.colorBgLayout ?? customBg),
      '--login-card': String(token.colorBgContainer ?? customBg),
      '--login-text': String(token.colorText ?? '#ffffff'),
      '--login-text-subtle': String(token.colorTextSecondary ?? 'rgba(255, 255, 255, 0.6)'),
      '--login-border': String(token.colorBorder ?? 'rgba(255, 255, 255, 0.14)'),
      '--login-accent': accent,
    } as CSSProperties;
  }, [themeName, antdThemeConfig, customBg, accent]);

  const langMenuItems = useMemo(
    () =>
      (LanguageManager.supportedLanguages as { value: string; name: string; icon: string }[]).map(
        (l) => ({
          key: l.value,
          label: (
            <Space size={8}>
              <span aria-hidden="true">{l.icon}</span>
              <span>{l.name}</span>
            </Space>
          ),
        }),
      ),
    [],
  );

  const themeIcon = !isDark ? (
    <SunOutlined aria-hidden />
  ) : !isUltra ? (
    <MoonOutlined aria-hidden />
  ) : (
    <MoonFilled aria-hidden />
  );

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass} style={themeVars}>
        <Layout.Content className="login-content">
          <div className="login-toolbar">
            <Button
              id="login-theme-picker"
              shape="circle"
              size="large"
              className="toolbar-btn"
              aria-label={t('menu.theme')}
              title={t('menu.theme')}
              icon={themeIcon}
              onClick={() => setThemePickerOpen(true)}
            />
            <Popover
              rootClassName={isDark ? 'dark' : 'light'}
              placement="bottomRight"
              trigger="click"
              styles={{ content: { padding: 4 } }}
              content={
                <Menu
                  mode="vertical"
                  selectable
                  selectedKeys={[lang]}
                  items={langMenuItems}
                  onClick={({ key }) => onLangChange(key)}
                  style={{ border: 'none', minWidth: 160 }}
                />
              }
            >
              <Button
                shape="circle"
                size="large"
                className="toolbar-btn"
                aria-label={t('pages.settings.language')}
                icon={<TranslationOutlined aria-hidden />}
              />
            </Popover>
          </div>

          <div className="login-wrapper">
            {!fetched ? (
              <div className="login-loading">
                <Spin size="large" />
              </div>
            ) : (
              <div className="login-card">
                <div className="brand">
                  <span className="brand-name">EX3-UI</span>
                  <span className="brand-accent" aria-hidden="true" />
                </div>
                <h2 className="welcome">
                  <b key={headlineIndex}>{headlineWords[headlineIndex]}</b>
                </h2>

                <FormProvider {...methods}>
                  <Form
                    layout="vertical"
                    className="login-form"
                    onFinish={methods.handleSubmit(onSubmit)}
                  >
                    <FormField
                      name="username"
                      label={t('username')}
                      rules={{ validate: rhfZodValidate(LoginFormSchema.shape.username) }}
                    >
                      <Input
                        prefix={<UserOutlined aria-hidden />}
                        autoComplete="username"
                        size="large"
                        placeholder={t('username')}
                        autoFocus
                      />
                    </FormField>

                    <FormField
                      name="password"
                      label={t('password')}
                      rules={{ validate: rhfZodValidate(LoginFormSchema.shape.password) }}
                    >
                      <Input.Password
                        prefix={<LockOutlined aria-hidden />}
                        autoComplete="current-password"
                        size="large"
                        placeholder={t('password')}
                      />
                    </FormField>

                    {twoFactorEnable && (
                      <FormField
                        name="twoFactorCode"
                        label={t('twoFactorCode')}
                        rules={{ validate: rhfZodValidate(TwoFactorCodeSchema) }}
                      >
                        <Input
                          prefix={<KeyOutlined aria-hidden />}
                          autoComplete="one-time-code"
                          size="large"
                          placeholder={t('twoFactorCode')}
                        />
                      </FormField>
                    )}

                    <Form.Item className="submit-row">
                      <Button
                        type="primary"
                        htmlType="submit"
                        loading={submitting}
                        size="large"
                        block
                      >
                        {t('login')}
                      </Button>
                    </Form.Item>
                  </Form>
                </FormProvider>
              </div>
            )}
          </div>
        </Layout.Content>
      </Layout>
      {/* Same picker as the panel: the login page must offer every fork theme,
          not only the upstream light/dark/ultra cycle. */}
      <ThemePickerModal open={themePickerOpen} onClose={() => setThemePickerOpen(false)} />
    </ConfigProvider>
  );
}
