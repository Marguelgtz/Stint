export type SparkCriticality = "low" | "medium" | "high";

export interface SparkAreaProfile {
  id: string;
  paths: string[];
  criticality: SparkCriticality;
  owners: string[];
  expectedEvidence: string[];
}

export interface SparkRepositoryProfile {
  version: 1;
  areas: SparkAreaProfile[];
}

/**
 * Stint's checked-in Spark repository profile.
 *
 * Keep this source-of-truth synchronized with /.spark/profile.yml. The test in
 * this package fails if the generated representation drifts from the committed
 * profile consumed by the Spark GitHub App.
 */
export const STINT_SPARK_PROFILE: SparkRepositoryProfile = {
  version: 1,
  areas: [
    {
      id: "interactive-control-plane",
      paths: ["apps/cli/**", "packages/core/**", "packages/router/**"],
      criticality: "high",
      owners: ["@Marguelgtz"],
      expectedEvidence: ["typecheck", "unit-tests"],
    },
    {
      id: "vast-compute-provider",
      paths: ["packages/provider-vast/**"],
      criticality: "high",
      owners: ["@Marguelgtz"],
      expectedEvidence: ["typecheck", "unit-tests"],
    },
    {
      id: "model-runtime",
      paths: ["packages/runtime-llama/**"],
      criticality: "high",
      owners: ["@Marguelgtz"],
      expectedEvidence: ["typecheck", "unit-tests"],
    },
    {
      id: "spark-collaboration",
      paths: ["packages/spark/**", "packages/contracts/**"],
      criticality: "high",
      owners: ["@Marguelgtz"],
      expectedEvidence: ["typecheck", "unit-tests", "spark-profile"],
    },
    {
      id: "fallback-inference",
      paths: ["packages/provider-cloudflare/**"],
      criticality: "medium",
      owners: ["@Marguelgtz"],
      expectedEvidence: ["typecheck", "unit-tests"],
    },
    {
      id: "release-and-automation",
      paths: [
        ".github/workflows/**",
        ".spark/profile.yml",
        "package.json",
        "pnpm-workspace.yaml",
        "stint.config.example.yaml",
      ],
      criticality: "high",
      owners: ["@Marguelgtz"],
      expectedEvidence: ["spark-profile", "typecheck", "unit-tests"],
    },
    {
      id: "developer-documentation",
      paths: ["docs/**", "README.md"],
      criticality: "low",
      owners: ["@Marguelgtz"],
      expectedEvidence: ["spark-profile"],
    },
  ],
};

function quote(value: string): string {
  return JSON.stringify(value);
}

export function serializeSparkProfile(profile: SparkRepositoryProfile): string {
  const lines: string[] = [`version: ${profile.version}`, "", "areas:"];

  for (const area of profile.areas) {
    lines.push(`  - id: ${area.id}`);
    lines.push("    paths:");
    for (const path of area.paths) lines.push(`      - ${quote(path)}`);
    lines.push(`    criticality: ${area.criticality}`);
    lines.push("    owners:");
    for (const owner of area.owners) lines.push(`      - ${quote(owner)}`);
    lines.push("    expected_evidence:");
    for (const evidence of area.expectedEvidence) lines.push(`      - ${evidence}`);
    lines.push("");
  }

  return `${lines.join("\n").trimEnd()}\n`;
}

export interface SparkOnboardingPlan {
  profilePath: ".spark/profile.yml";
  dashboardUrl: string;
  expectedEvidence: string[];
  steps: string[];
}

export function createSparkOnboardingPlan(
  dashboardUrl = "https://spark-api.marguel-gtz.workers.dev/app",
): SparkOnboardingPlan {
  const expectedEvidence = [
    ...new Set(STINT_SPARK_PROFILE.areas.flatMap((area) => area.expectedEvidence)),
  ].sort();

  return {
    profilePath: ".spark/profile.yml",
    dashboardUrl,
    expectedEvidence,
    steps: [
      "Create/push the Stint GitHub repository.",
      "Install the Spark GitHub App for that repository from the Spark dashboard.",
      "Keep .spark/profile.yml committed on the default branch.",
      "Confirm GitHub Actions emits the expected evidence checks.",
      "Open a pull request and confirm Spark posts an evaluation check for the exact head SHA.",
    ],
  };
}
