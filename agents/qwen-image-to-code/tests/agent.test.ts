import assert from "node:assert/strict";
import test from "node:test";
import { qwenImageToCodeAgent } from "../src/agent.ts";

test("exports the independent Qwen image-to-code agent", () => {
  assert.equal(qwenImageToCodeAgent.id, "qwen_image-to-code");
  assert.equal(qwenImageToCodeAgent.name, "Qwen_image-to-code");
});
