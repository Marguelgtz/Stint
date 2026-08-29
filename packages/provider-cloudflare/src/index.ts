export interface CloudflareFallbackConfig {
  accountId: string;
  modelId: string;
  baseUrl: string;
}

export function cloudflareWorkersAiBaseUrl(accountId: string): string {
  return `https://api.cloudflare.com/client/v4/accounts/${accountId}/ai/v1`;
}
