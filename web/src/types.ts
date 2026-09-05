export type PeerStatus = {
  id: string;
  name: string;
  endpoint: string;
  latestHandshake: number;
  receiveBytes: number;
  transmitBytes: number;
  connected: boolean;
};

export type GatewayStatus = {
  publicIp: string;
  egressInterface: string;
  wireGuardPort: number;
  realityPort: number;
  wireGuardUp: boolean;
  xrayUp: boolean;
  dnsUp: boolean;
  ipv4Forwarding: boolean;
  peers: PeerStatus[];
};

export type Client = {
  id: string;
  name: string;
  createdAt: string;
  ipv4: string;
  ipv6: string;
};
