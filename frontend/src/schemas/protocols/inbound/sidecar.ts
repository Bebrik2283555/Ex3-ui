import { z } from 'zod';

// The three protocols in this module are served by external cores (qWDTT,
// olcRTC, CSQTT), not by Xray: the panel stores their whole runtime
// configuration under `settings.sidecar` and the Go side writes it out for the
// core. One client schema serves all three, so a client attached to both a
// qWDTT and a CSQTT inbound carries both per-core passwords.

export const SidecarClientSchema = z.object({
  email: z.string().min(1),
  // `password` is the generic client password; legacy qWDTT links were built
  // from it before the per-core passwords existed.
  password: z.string().default(''),
  qwdttPassword: z.string().default(''),
  csqttPassword: z.string().default(''),
  limitIp: z.number().int().min(0).default(0),
  totalGB: z.number().int().min(0).default(0),
  reset: z.number().int().min(0).default(0),
  expiryTime: z.number().int().default(0),
  enable: z.boolean().default(true),
  // Telegram ids arrive as numbers or numeric strings; anything else is "none".
  tgId: z
    .union([z.number(), z.string()])
    .transform((v) => Number(v) || 0)
    .default(0),
  subId: z.string().default(''),
  comment: z.string().default(''),
  created_at: z.number().int().optional(),
  updated_at: z.number().int().optional(),
});
export type SidecarClient = z.infer<typeof SidecarClientSchema>;

// qWDTT. `listenAddr` is the DTLS listener advertised to clients,
// `subHost` the host used in generated subscription links, `hashes` the
// default call hashes (optionally overridden per client via
// `hashesByEmail`). `mainPassword` and `routeXrayPort` are owned by the
// backend; the editor never writes them.
export const QwdttSettingsSchema = z.object({
  listenAddr: z.string().default('0.0.0.0:56000'),
  wgPort: z.number().int().min(1).max(65535).default(56001),
  listenRaw: z.string().default('0.0.0.0:56003'),
  listenDirect: z.string().default(''),
  hashes: z.string().default(''),
  hashesByEmail: z.record(z.string(), z.string()).default({}),
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
  // A save always writes the complete block, so the core never has to guess a
  // default; parsing `{}` is exactly that block.
  sidecar: QwdttSettingsSchema.optional().transform(
    (value) => value ?? QwdttSettingsSchema.parse({}),
  ),
});
export type QwdttInboundSettings = z.infer<typeof QwdttInboundSettingsSchema>;

// olcRTC. The provider decides which transports exist; the tuning numbers only
// mean something for the transport that is selected. `dataDir`/`configFile`
// are backend-managed and never edited.
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
  sidecar: OlcrtcSettingsSchema.optional().transform(
    (value) => value ?? OlcrtcSettingsSchema.parse({}),
  ),
});
export type OlcrtcInboundSettings = z.infer<typeof OlcrtcInboundSettingsSchema>;

// CSQTT. The core runs its own web API (`webPort`/`webUser`/`webPass`, kept
// out of the editor) next to the UDP listener clients dial.
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
  hashesByEmail: z.record(z.string(), z.string()).default({}),
  subHost: z.string().default(''),
  routeThroughXray: z.boolean().default(true),
  outboundTag: z.string().default(''),
  routeXrayPort: z.number().int().min(0).max(65535).default(0),
});
export type CsqttSettings = z.infer<typeof CsqttSettingsSchema>;

export const CsqttInboundSettingsSchema = z.object({
  clients: z.array(SidecarClientSchema).default([]),
  sidecar: CsqttSettingsSchema.optional().transform(
    (value) => value ?? CsqttSettingsSchema.parse({}),
  ),
});
export type CsqttInboundSettings = z.infer<typeof CsqttInboundSettingsSchema>;
