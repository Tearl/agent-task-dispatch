import assert from "node:assert/strict";
import test from "node:test";
import { glmImageToCodeAgent } from "../src/agent.ts";

test("exports the independent GLM image-to-code agent", () => {
  assert.equal(glmImageToCodeAgent.id, "glm_image-to-code");
  assert.equal(glmImageToCodeAgent.name, "glm_image-to-code");
});
