export interface SaasBridgeCapabilities {
  tabs?: boolean;
  storage?: boolean;
  fingerprint?: boolean;
  files?: boolean;
  http?: boolean;
  crypto?: boolean;
}

export interface SaasBridgeDetails {
  version: string;
  origin?: string;
  capabilities: SaasBridgeCapabilities;
}

export interface SaasFileEntry {
  name: string;
  path: string;
  directory: boolean;
  /** 文档提供方无法返回大小时为 -1。 */
  size: number;
}

export interface SaasHttpResponse {
  status: number;
  headers: Record<string, string>;
  /** 安卓保留重复响应头；其他平台可以不返回。 */
  headersList?: Array<{name: string; value: string}>;
  bodyBase64: string;
}

export interface SaasSnapshotEnvelope {
  algorithm: 'AES-256-GCM';
  kdf: 'PBKDF2-HMAC-SHA-256';
  iterations: number;
  salt: string;
  nonce: string;
  ciphertext: string;
  tag: string;
}

export interface SaasBridge {
  /** Major version changes are incompatible. */
  readonly version: `${number}.${number}`;
  readonly origin?: string;
  getCapabilities?(): Promise<SaasBridgeDetails> | SaasBridgeDetails;
  readonly capabilities?: SaasBridgeCapabilities;
  tabs: {
    list(): Promise<unknown>;
    create(options: {
      accountId?: string;
      url?: string;
      proxyRules?: string;
      fingerprintSeed?: string;
      fingerprint?: {user_agent?: string; hardware_concurrency?: number};
    }): Promise<unknown>;
    activate(options: { tabId: string }): Promise<unknown>;
    navigate(options: { tabId: string; url: string }): Promise<unknown>;
    close(options: { tabId: string }): Promise<unknown>;
  };
  storage: {
    getSnapshot(options: { tabId: string } & Record<string, unknown>): Promise<unknown>;
    writeSnapshot(options: { tabId: string; snapshot: unknown } & Record<string, unknown>): Promise<unknown>;
  };
  fingerprint: {
    get(options: { tabId: string }): Promise<unknown>;
    set(options: { tabId: string; fingerprint: unknown }): Promise<unknown>;
  };
  files: {
    list(options?: {path?: string}): Promise<SaasFileEntry[]>;
    read(options: {path: string}): Promise<{
      path: string;
      size: number;
      dataBase64: string;
    }>;
    write(options: {path: string; dataBase64: string}): Promise<{
      path: string;
      size: number;
      /** 安卓可恢复覆盖保留的原文件路径；新文件或其他平台可以不返回。 */
      backupPath?: string;
    }>;
  };
  http: {
    request(options: {
      url: string;
      method?: string;
      headers?: Record<string, string>;
      bodyBase64?: string;
      contentType?: string;
      includeCredentials?: boolean;
      /** 安卓自动携带凭据时必须指定独立账号 Tab，不使用共享 Profile。 */
      tabId?: string;
    }): Promise<SaasHttpResponse>;
  };
  crypto?: {
    encryptSnapshot(options: {accountId: string; password: string; snapshot: unknown}): Promise<SaasSnapshotEnvelope>;
    decryptSnapshot(options: {accountId: string; password: string; envelope: SaasSnapshotEnvelope}): Promise<unknown>;
  };
}

export interface SaasBridgeClient {
  isAvailable(): boolean;
  describe(): Promise<{
    available: boolean;
    version: string;
    origin: string;
    capabilities: SaasBridgeCapabilities;
    reason?: string;
  }>;
  tabs: {
    list(): Promise<unknown>;
    create(options: Parameters<SaasBridge['tabs']['create']>[0]): Promise<unknown>;
    activate(tabId: string): Promise<unknown>;
    navigate(tabId: string, url: string): Promise<unknown>;
    close(tabId: string): Promise<unknown>;
  };
  storage: {
    getSnapshot(tabId: string, options?: Record<string, unknown>): Promise<unknown>;
    writeSnapshot(
        tabId: string, snapshot: unknown,
        options?: Record<string, unknown>): Promise<unknown>;
  };
  fingerprint: {
    get(tabId: string): Promise<unknown>;
    set(tabId: string, fingerprint: unknown): Promise<unknown>;
  };
  files: SaasBridge['files'];
  http: SaasBridge['http'];
  crypto: NonNullable<SaasBridge['crypto']>;
}

declare global {
  interface Window {
    saasBridge?: SaasBridge;
    saasBridgeClient: SaasBridgeClient;
  }
}

export {};
