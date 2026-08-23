import { createHmac } from "node:crypto";
import type { ExecutionCallback } from "./execution-protocol.ts";

export interface ExecutionCallbackSender {
  send(url: string, callback: ExecutionCallback): Promise<void>;
}

export class HmacExecutionCallbackSender implements ExecutionCallbackSender {
  private readonly key: Buffer;

  constructor(key: Buffer) {
    if (key.byteLength < 32) throw new Error("callback key must contain at least 32 bytes");
    this.key = key;
  }

  async send(url: string, callback: ExecutionCallback): Promise<void> {
    const target = new URL(url);
    if (target.protocol !== "https:" || target.username || target.password) {
      throw new Error("callback URL must use HTTPS without credentials");
    }
    const body = JSON.stringify(callback);
    const signature = `hmac-sha256=${createHmac("sha256", this.key).update(body).digest("hex")}`;
    const response = await fetch(target, {
      method: "POST",
      redirect: "error",
      headers: { "Content-Type": "application/json", "X-Agent-Signature": signature },
      body,
      signal: AbortSignal.timeout(10_000),
    });
    if (!response.ok) throw new Error(`Engine callback returned ${response.status}`);
  }
}

export class NoopExecutionCallbackSender implements ExecutionCallbackSender {
  async send(): Promise<void> {}
}
