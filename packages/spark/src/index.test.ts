import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  STINT_SPARK_PROFILE,
  createSparkOnboardingPlan,
  serializeSparkProfile,
} from "./index.js";

const checkedInProfile = readFileSync(
  new URL("../../../.spark/profile.yml", import.meta.url),
  "utf8",
);

describe("Stint Spark profile", () => {
  it("matches the checked-in .spark/profile.yml", () => {
    expect(serializeSparkProfile(STINT_SPARK_PROFILE)).toBe(checkedInProfile);
  });

  it("keeps every area owned and evidence-backed", () => {
    for (const area of STINT_SPARK_PROFILE.areas) {
      expect(area.owners).toContain("@Marguelgtz");
      expect(area.paths.length).toBeGreaterThan(0);
      expect(area.expectedEvidence.length).toBeGreaterThan(0);
    }
  });

  it("includes the CI evidence required to onboard the repo", () => {
    expect(createSparkOnboardingPlan().expectedEvidence).toEqual([
      "spark-profile",
      "typecheck",
      "unit-tests",
    ]);
  });
});
