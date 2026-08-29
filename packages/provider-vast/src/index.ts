import type { Offer, Profile } from "@stint/core";

export interface ComputeProvider {
  searchOffers(profile: Profile): Promise<Offer[]>;
}

/**
 * Deliberately dry-run only in the boiler.
 * The first real adapter should call Vast's API/CLI and normalize results into Offer[].
 */
export class VastProvider implements ComputeProvider {
  async searchOffers(_profile: Profile): Promise<Offer[]> {
    throw new Error("VastProvider.searchOffers is not implemented yet; use dry-run fixtures first");
  }
}
