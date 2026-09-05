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
export type PolicyRoute = "ai_fixed" | "japan";

export interface PolicyRouteSummary {
  route: PolicyRoute;
  count: number;
}

export interface PolicyConnection {
  id: string;
  processPath: string;
  host?: string;
  destinationIp?: string;
  destinationPort: string;
  network: string;
  disposition: PolicyRoute;
  observedOutbound?: string;
  reason: string;
  isAiProcess: boolean;
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
  routeSummary: PolicyRouteSummary[];
  violationCount: number;
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
}
