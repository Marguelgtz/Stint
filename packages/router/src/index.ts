import { BUILTIN_PROFILES, type Profile } from "@stint/core";

export function resolveProfile(name: string): Profile {
  const profile = BUILTIN_PROFILES[name];
  if (!profile) throw new Error(`Unknown profile: ${name}`);
  return profile;
}
