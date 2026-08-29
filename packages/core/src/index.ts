export type Objective = "latency" | "validated_work_per_dollar";

export interface GpuPolicy {
  preferredModels: string[];
  maxHourlyUsd: number;
  minReliability: number;
}

export interface Profile {
  name: string;
  objective: Objective;
  workers: number;
  gpu: GpuPolicy;
}

export interface Offer {
  id: string;
  gpuModel: string;
  hourlyUsd: number;
  reliability: number;
  dlperf?: number;
}

export interface PlannedWorker {
  offer: Offer;
  estimatedSessionUsd: number;
}

export interface SessionPlan {
  profile: Profile;
  hours: number;
  workers: PlannedWorker[];
  estimatedTotalUsd: number;
}

export const BUILTIN_PROFILES: Record<string, Profile> = {
  interactive: {
    name: "interactive",
    objective: "latency",
    workers: 1,
    gpu: {
      preferredModels: ["RTX_4090", "RTX_5090"],
      maxHourlyUsd: 0.4,
      minReliability: 0.985,
    },
  },
  deep: {
    name: "deep",
    objective: "validated_work_per_dollar",
    workers: 2,
    gpu: {
      preferredModels: ["RTX_3090"],
      maxHourlyUsd: 0.18,
      minReliability: 0.98,
    },
  },
};

export function selectOffers(profile: Profile, offers: Offer[]): Offer[] {
  const preferred = new Map(profile.gpu.preferredModels.map((gpu, i) => [gpu, i]));

  return offers
    .filter((offer) =>
      preferred.has(offer.gpuModel) &&
      offer.hourlyUsd <= profile.gpu.maxHourlyUsd &&
      offer.reliability >= profile.gpu.minReliability
    )
    .sort((a, b) => {
      const gpuRank = (preferred.get(a.gpuModel) ?? 99) - (preferred.get(b.gpuModel) ?? 99);
      if (gpuRank !== 0) return gpuRank;

      if (profile.objective === "latency") {
        const perfA = a.dlperf ?? 0;
        const perfB = b.dlperf ?? 0;
        if (perfA !== perfB) return perfB - perfA;
      }

      return a.hourlyUsd - b.hourlyUsd;
    })
    .slice(0, profile.workers);
}

export function createSessionPlan(profile: Profile, hours: number, offers: Offer[]): SessionPlan {
  if (!Number.isFinite(hours) || hours <= 0) throw new Error("hours must be greater than zero");

  const selected = selectOffers(profile, offers);
  if (selected.length < profile.workers) {
    throw new Error(`Need ${profile.workers} qualifying worker(s), found ${selected.length}`);
  }

  const workers = selected.map((offer) => ({
    offer,
    estimatedSessionUsd: Number((offer.hourlyUsd * hours).toFixed(2)),
  }));

  return {
    profile,
    hours,
    workers,
    estimatedTotalUsd: Number(workers.reduce((sum, worker) => sum + worker.estimatedSessionUsd, 0).toFixed(2)),
  };
}
