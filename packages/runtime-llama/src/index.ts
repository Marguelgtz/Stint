export interface RuntimeSpec {
  modelId: string;
  contextTokens: number;
  flashAttention: boolean;
  mtp: boolean;
}

export const QWEN38_27B_INTERACTIVE: RuntimeSpec = {
  modelId: "qwen3.8-27b",
  contextTokens: 65536,
  flashAttention: true,
  mtp: true,
};
