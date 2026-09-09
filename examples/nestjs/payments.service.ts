// Reference: a feature service consuming KnobsService — typed, live config.

import { Injectable } from "@nestjs/common";
import { KnobsService } from "./knobs.module.js";

@Injectable()
export class PaymentsService {
  constructor(private readonly knobs: KnobsService) {}

  async charge(amountMinor: number): Promise<void> {
    const cfg = this.knobs.config(); // typed AppConfig, always current

    if (!cfg.featureX) {
      throw new Error("payments feature disabled by config");
    }

    let attempt = 0;
    // cfg.maxRetries is a compile-checked number.
    while (attempt <= cfg.maxRetries) {
      try {
        await this.callGateway(amountMinor);
        return;
      } catch (err) {
        if (attempt === cfg.maxRetries) throw err;
        attempt++;
      }
    }
  }

  private async callGateway(_amountMinor: number): Promise<void> {
    // ... real gateway call ...
  }
}
