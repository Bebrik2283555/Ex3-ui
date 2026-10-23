import { z } from 'zod';

// qWDTT, olcRTC and CSQTT are served by external binaries next to Xray, so
// their settings never reach the Xray config. The panel keeps them in
// settings.sidecar and mirrors the client list into each core's own store.

export const SidecarClientSchema = z.object({
  email: z.string().min(1),
  password: z.string().default(''),
  qwdttPassword: z.string().default(''),
  csqttPassword: z.string().default(''),
  limitIp: z.number().int().min(0).default(0),
  totalGB: z.number().int().min(0).default(0),
  expiryTime: z.number().int().default(0),
  enable: z.boolean().default(true),
  tgId: z
    .union([z.number(), z.string()])
    .transform((v) => Number(v) || 0)
    .default(0),
  subId: z.string().default(''),
  comment: z.string().default(''),
  reset: z.number().int().min(0).default(0),
  created_at: z.number().int().optional(),
  updated_at: z.number().int().optional(),
});
export type SidecarClient = z.infer<typeof SidecarClientSchema>;

const hashesByEmail = z.record(z.string(), z.string()).default({});

export const QwdttSettingsSchema = z.object({
  listenAddr: z.string().default('0.0.0.0:56000'),
  wgPort: z.number().int().min(1).max(65535).default(56001),
  listenRaw: z.string().default('0.0.0.0:56003'),
  listenDirect: z.string().default(''),
  hashes: z.string().default(''),
  hashesByEmail,
  workers: z.number().int().min(1).max(256).default(16),
  clientPort: z.number().int().min(1).max(65535).default(9000),
  dns: z.string().default('1.1.1.1'),
  subHost: z.string().default(''),
  mainPassword: z.string().default(''),
  configDir: z.string().default(''),
  routeThroughXray: z.boolean().default(true),
  outboundTag: z.string().default(''),
  routeXrayPort: z.number().int().min(0).max(65535).default(0),
});
export type QwdttSettings = z.infer<typeof QwdttSettingsSchema>;

export const QwdttInboundSettingsSchema = z.object({
  clients: z.array(SidecarClientSchema).default([]),
  sidecar: QwdttSettingsSchema.default(QwdttSettingsSchema.parse({})),
});
export type QwdttInboundSettings = z.infer<typeof QwdttInboundSettingsSchema>;

export const OlcrtcSettingsSchema = z.object({
  provider: z.enum(['jitsi', 'telemost', 'wbstream']).default('jitsi'),
  roomId: z.string().default(''),
  cryptoKey: z.string().default(''),
  transport: z
    .enum(['datachannel', 'vp8channel', 'seichannel', 'videochannel'])
    .default('datachannel'),
  vp8Fps: z.number().int().min(1).max(120).default(60),
  vp8Batch: z.number().int().min(1).max(64).default(64),
  seiFps: z.number().int().min(1).max(120).default(30),
  seiBatch: z.number().int().min(1).max(64).default(64),
  seiFrag: z.number().int().min(1).default(900),
  seiAck: z.number().int().min(1).default(2000),
  videoW: z.number().int().min(1).default(1080),
  videoH: z.number().int().min(1).default(1080),
  videoFps: z.number().int().min(1).max(120).default(30),
  videoCodec: z.enum(['qrcode', 'tile']).default('qrcode'),
  dns: z.string().default('8.8.8.8:53'),
  dataDir: z.string().default(''),
  configFile: z.string().default(''),
  debug: z.boolean().default(false),
  routeThroughXray: z.boolean().default(true),
  outboundTag: z.string().default(''),
  routeXrayPort: z.number().int().min(0).max(65535).default(0),
});
export type OlcrtcSettings = z.infer<typeof OlcrtcSettingsSchema>;

export const OlcrtcInboundSettingsSchema = z.object({
  clients: z.array(SidecarClientSchema).default([]),
  sidecar: OlcrtcSettingsSchema.default(OlcrtcSettingsSchema.parse({})),
});
export type OlcrtcInboundSettings = z.infer<typeof OlcrtcInboundSettingsSchema>;

export const CsqttSettingsSchema = z.object({
  listenAddr: z.string().default('0.0.0.0:46000'),
  webPort: z.number().int().min(1).max(65535).default(46002),
  webUser: z.string().default('ex3ui'),
  webPass: z.string().default(''),
  dns: z.string().default('77.88.8.8,77.88.8.1'),
  configDir: z.string().default(''),
  mainPassword: z.string().default(''),
  secureCookie: z.boolean().default(false),
  webSecretPath: z.string().default(''),
  hashes: z.string().default(''),
  hashesByEmail,
  subHost: z.string().default(''),
  routeThroughXray: z.boolean().default(true),
  outboundTag: z.string().default(''),
  routeXrayPort: z.number().int().min(0).max(65535).default(0),
});
export type CsqttSettings = z.infer<typeof CsqttSettingsSchema>;

export const CsqttInboundSettingsSchema = z.object({
  clients: z.array(SidecarClientSchema).default([]),
  sidecar: CsqttSettingsSchema.default(CsqttSettingsSchema.parse({})),
});
export type CsqttInboundSettings = z.infer<typeof CsqttInboundSettingsSchema>;
