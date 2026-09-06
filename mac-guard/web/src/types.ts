export type Health = "healthy" | "degraded" | "failed" | "unknown";
export type GuardState = "boot_locked" | "tunnel_connecting" | "healthy" | "degraded_locked" | "unprotected" | "recovering" | "maintenance";

export interface ComponentStatus {
  id: string;
  name: string;
  health: Health;
  required: boolean;
  summary: string;
  detail: string;
  lastCheckedAt: string;
  latencyMs: number;
}

export interface Stage {
  id: string;
  name: string;
  health: Health;
  summary: string;
}

export interface TestResult {
  id: string;
  name: string;
  health: Health;
  expected: string;
  actual: string;
  detail: string;
  durationMs: number;
  runAt: string;
}

export interface Alert {
  id: string;
  severity: "critical" | "warning" | "info";
  title: string;
  message: string;
  component: string;
  raisedAt: string;
}

export interface GuardEvent {
  id: number;
  time: string;
  severity: string;
  component: string;
  message: string;
}

export type PolicyMode = "enforce";
export type PolicyProfile = "global_japan";

export interface PolicyConnection {
  id: string;
  processPath: string;
  host?: string;
  destinationIp?: string;
  destinationPort: string;
  network: string;
  observedOutbound?: string;
  compliant: boolean;
  upload: number;
  download: number;
  startedAt: string;
}

export interface PolicyV2Snapshot {
  enabled: boolean;
  mode: PolicyMode;
  profile: PolicyProfile;
  observerHealth: Health;
  observerMessage: string;
  lastPollAt?: string;
  lastSuccessfulPollAt?: string;
  activeConnections: PolicyConnection[];
  violationCount: number;
}

export interface RuntimeConfiguration {
  profile: string;
  transport: string;
  vpsAddress: string;
  vpsPort: number;
  expectedEgress: string;
  tunnelAddress: string;
  tunnelInterfacePrefix: string;
  expectedDnsServers: string[];
  pfAnchor: string;
  pfAnchorFile: string;
  pfMainConfigFile: string;
  guardService: string;
  providerService: string;
  listenAddress: string;
  authenticationEnabled: boolean;
  checkIntervalSeconds: number;
  conflictCheckIntervalSeconds: number;
  observerUrl: string;
  observerIntervalSeconds: number;
  components: RuntimeComponentConfiguration[];
}

export interface RuntimeComponentConfiguration {
  id: string;
  name: string;
  owner: string;
  launchdLabel?: string;
  executable?: string;
  arguments?: string[];
  settings: RuntimeSetting[];
  healthComponentIds: string[];
}

export interface RuntimeSetting {
  name: string;
  value: string;
}

export interface NodeConfiguration {
  server: string;
  port: number;
  expectedEgress: string;
  flow: string;
  tlsServerName: string;
  utlsFingerprint: string;
  uuidConfigured: boolean;
  realityPublicKeyConfigured: boolean;
  realityShortIdConfigured: boolean;
}

export interface NodeUpdate {
  server: string;
  port: number;
  expectedEgress: string;
  flow: string;
  tlsServerName: string;
  utlsFingerprint: string;
  uuid?: string;
  realityPublicKey?: string;
  realityShortId?: string;
}

export interface Snapshot {
  version: string;
  state: GuardState;
  stateSince: string;
  protected: boolean;
  connected: boolean;
  provider: string;
  expectedEgress: string;
  observedEgress: string;
  physicalInterface: string;
  tunnelInterface: string;
  lastCheckAt: string;
  nextCheckAt: string;
  checking: boolean;
  recovering: boolean;
  components: ComponentStatus[];
  stages: Stage[];
  tests: TestResult[];
  alerts: Alert[];
  events: GuardEvent[];
  policyV2?: PolicyV2Snapshot;
  runtimeConfig: RuntimeConfiguration;
}
