// Display aliases only: full provider and target names remain in titles/ARIA.
const providers: Record<string, string> = {
  'Epic Games': 'EP', 'Amazon Web Services': 'AWS', 'User-provided': 'LOCAL', 'Amazon EC': 'AEC', 'Amazon EC2': 'EC2',
  'Digital Ocean': 'DO', DigitalOcean: 'DO', 'G-Core Labs': 'GCORE',
  BinaryLane: 'BLANE', Cloudflare: 'CF', CloudHosting: 'CHOST',
  CloudSigma: 'CSIG', HostHatch: 'HH', HostingInside: 'HIN',
  IndoVirtue: 'IV', 'KT01 IDC': 'KT01', LeaseWeb: 'LW',
  LightNode: 'LN', OneProvider: 'OP', RansomIT: 'RIT',
  'SK Broadband': 'SKB', VPSMalaysia: 'VPSMY',
  Google: 'GOOG', Tencent: 'TENC', VinaHost: 'VH', Viettel: 'VTL',
  Rackmill: 'RACK', Azure: 'AZR', Linode: 'LIN',
};
export function compactProvider(provider: string) {
  const match = provider.match(/^(.*?) (\d+)$/);
  const base = match?.[1] ?? provider, suffix = match ? ` ${match[2]}` : '';
  return (providers[base] ?? base.toUpperCase()) + suffix;
}

// A positive rate must not round down to a displayed zero.
export function compactLoss(value: number | null | undefined) {
  if (value == null) return '?';
  if (value > 0 && value < 0.1) return '<0.1%';
  return `${Number(value.toFixed(1))}%`;
}
