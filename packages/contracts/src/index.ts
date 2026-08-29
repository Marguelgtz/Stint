export type WorkerMode = "interactive" | "deep" | "sleep";

export interface WorkerEndpoint {
  workerId: string;
  mode: WorkerMode;
  baseUrl: string;
  modelId: string;
  hourlyUsd: number;
}

export interface CollaborationAdapter {
  registerWorker(endpoint: WorkerEndpoint): Promise<void>;
  retireWorker(workerId: string): Promise<void>;
}
