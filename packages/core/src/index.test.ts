import { describe, expect, it } from "vitest";
import { BUILTIN_PROFILES, createSessionPlan } from "./index.js";

describe("session planning", () => {
  it("chooses a fast qualifying 4090 for interactive work", () => {
    const plan = createSessionPlan(BUILTIN_PROFILES.interactive, 5, [
      { id: "cheap", gpuModel: "RTX_4090", hourlyUsd: 0.31, reliability: 0.99, dlperf: 95 },
      { id: "fast", gpuModel: "RTX_4090", hourlyUsd: 0.35, reliability: 0.995, dlperf: 110 },
    ]);
    expect(plan.workers[0].offer.id).toBe("fast");
    expect(plan.estimatedTotalUsd).toBe(1.75);
  });

  it("chooses two inexpensive 3090s for deep work", () => {
    const plan = createSessionPlan(BUILTIN_PROFILES.deep, 8, [
      { id: "a", gpuModel: "RTX_3090", hourlyUsd: 0.14, reliability: 0.99 },
      { id: "b", gpuModel: "RTX_3090", hourlyUsd: 0.15, reliability: 0.99 },
      { id: "expensive", gpuModel: "RTX_3090", hourlyUsd: 0.19, reliability: 0.999 },
    ]);
    expect(plan.workers.map((w) => w.offer.id)).toEqual(["a", "b"]);
    expect(plan.estimatedTotalUsd).toBe(2.32);
  });
});
