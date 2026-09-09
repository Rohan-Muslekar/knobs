// Reference: a NestJS module that wires @knobs/sdk as a DI provider with a
// managed lifecycle (ready on boot, close on shutdown).
//
// Requires: @nestjs/common, @knobs/sdk. This is a reference snippet — drop it
// into a Nest app and `npm install @knobs/sdk`.

import {
  Global,
  Injectable,
  Module,
  type OnModuleInit,
  type OnApplicationShutdown,
} from "@nestjs/common";
import { createClient, type KnobsClient } from "@knobs/sdk";

// Your generated types (knobs gen --lang ts --out knobs.gen.ts).
import { typed, schemaHash, type AppConfig } from "./knobs.gen.js";

/**
 * KnobsService owns one KnobsClient for the process. It loads the first
 * snapshot during Nest bootstrap and closes the stream on shutdown, so the
 * rest of the app can read config synchronously and always get live values.
 */
@Injectable()
export class KnobsService implements OnModuleInit, OnApplicationShutdown {
  private client!: KnobsClient;

  async onModuleInit(): Promise<void> {
    this.client = createClient({
      endpoint: required("KNOBS_ENDPOINT"),
      apiKey: required("KNOBS_API_KEY"),
      environment: process.env.KNOBS_ENV ?? "prod",
      expectedSchemaHash: schemaHash,
    });
    await this.client.ready();
  }

  onApplicationShutdown(): void {
    this.client?.close();
  }

  /** Typed, always-current config snapshot. */
  config(): AppConfig {
    return typed(this.client);
  }

  /** Subscribe to live changes; returns an unsubscribe fn. */
  onChange(cb: (config: AppConfig) => void): () => void {
    return this.client.onChange(() => cb(this.config()));
  }
}

function required(name: string): string {
  const v = process.env[name];
  if (!v) throw new Error(`${name} is required`);
  return v;
}

@Global()
@Module({
  providers: [KnobsService],
  exports: [KnobsService],
})
export class KnobsModule {}
