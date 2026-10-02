export interface SaasBridgeCapabilities {
  tabs?: boolean;
  storage?: boolean;
  fingerprint?: boolean;
  files?: boolean;
  http?: boolean;
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
  size: number;
}

export interface SaasHttpResponse {
  status: number;
  headers: Record<string, string>;
  bodyBase64: string;
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
    }): Promise<SaasHttpResponse>;
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
}

declare global {
  interface Window {
    saasBridge?: SaasBridge;
    saasBridgeClient: SaasBridgeClient;
  }
}

export {};
