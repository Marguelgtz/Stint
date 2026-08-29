#!/usr/bin/env node
import { Command } from "commander";
import { createSessionPlan, type Offer } from "@stint/core";
import { resolveProfile } from "@stint/router";
import { createSparkOnboardingPlan } from "@stint/spark";

const program = new Command();
program.name("stint").description("Elastic compute for coding agents").version("0.0.1");

program
  .command("plan")
  .argument("<profile>", "interactive or deep")
  .option("--hours <hours>", "session duration", "5")
  .description("Create a dry-run compute plan")
  .action((profileName: string, options: { hours: string }) => {
    const profile = resolveProfile(profileName);
    const hours = Number(options.hours);

    // Fixture offers make the planner runnable before provider credentials are wired in.
    const offers: Offer[] = profileName === "interactive"
      ? [
          { id: "fixture-4090-fast", gpuModel: "RTX_4090", hourlyUsd: 0.35, reliability: 0.995, dlperf: 110 },
          { id: "fixture-4090-cheap", gpuModel: "RTX_4090", hourlyUsd: 0.31, reliability: 0.99, dlperf: 96 },
        ]
      : [
          { id: "fixture-3090-a", gpuModel: "RTX_3090", hourlyUsd: 0.14, reliability: 0.99, dlperf: 53 },
          { id: "fixture-3090-b", gpuModel: "RTX_3090", hourlyUsd: 0.15, reliability: 0.992, dlperf: 55 },
          { id: "fixture-3090-over", gpuModel: "RTX_3090", hourlyUsd: 0.2, reliability: 0.999, dlperf: 58 },
        ];

    const plan = createSessionPlan(profile, hours, offers);
    console.log(JSON.stringify(plan, null, 2));
  });

const onboard = program.command("onboard").description("Prepare integrations for the current repository");

onboard
  .command("spark")
  .option(
    "--dashboard <url>",
    "Spark dashboard URL",
    "https://spark-api.marguel-gtz.workers.dev/app",
  )
  .description("Print the Stint repository's Spark onboarding plan")
  .action((options: { dashboard: string }) => {
    const plan = createSparkOnboardingPlan(options.dashboard);
    console.log("Spark onboarding\n");
    console.log(`Profile: ${plan.profilePath}`);
    console.log(`Dashboard: ${plan.dashboardUrl}`);
    console.log(`Expected GitHub evidence: ${plan.expectedEvidence.join(", ")}`);
    console.log("\nSteps:");
    plan.steps.forEach((step, index) => console.log(`${index + 1}. ${step}`));
  });

program.parseAsync(process.argv).catch((error) => {
  console.error(error instanceof Error ? error.message : error);
  process.exitCode = 1;
});
