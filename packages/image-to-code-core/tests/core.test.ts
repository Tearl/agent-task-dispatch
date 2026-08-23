import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
  buildImageToCodeMessages,
  generatedProjectSchema,
  loadImageInput,
  parseImageToCodeExecutionInput,
  parseGeneratedProjectText,
  resolveProjectOutputDirectory,
  writeGeneratedProject,
} from "../src/index.ts";

test("loads a validated PNG and builds a multimodal message", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-to-code-"));
  const file = path.join(directory, "screen.png");
  const png = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1]);
  await writeFile(file, png);

  const image = await loadImageInput(file);
  const messages = buildImageToCodeMessages({ image, target: "React" });

  assert.equal(messages[0]?.content[1]?.type, "file");
  assert.equal(messages[0]?.content[1]?.data, png.toString("base64"));
});

test("validates platform image-to-code JSON input", () => {
  const png = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1]);
  const input = parseImageToCodeExecutionInput({
    image: { data: png.toString("base64"), filename: "screen.png", mediaType: "image/png" },
    target: "React",
  });
  assert.equal(input.image.mediaType, "image/png");
  assert.throws(() => parseImageToCodeExecutionInput({ image: { data: Buffer.from("fake").toString("base64"), filename: "fake.png", mediaType: "image/png" } }), /do not match/u);
});

test("rejects an invalid image signature", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-to-code-"));
  const file = path.join(directory, "fake.png");
  await writeFile(file, "not a png");
  await assert.rejects(loadImageInput(file), /do not match image\/png/);
});

test("rejects generated paths outside the project", () => {
  const base = {
    summary: "Page",
    assumptions: [],
    files: [{ path: "../secret", language: "text", content: "secret" }],
    runInstructions: [],
    caveats: [],
  };
  assert.equal(generatedProjectSchema.safeParse(base).success, false);
});

test("parses a fenced JSON project returned by a compatibility model", () => {
  const result = parseGeneratedProjectText(`\`\`\`json
  {"summary":"Page","assumptions":[],"files":[{"path":"src/App.tsx","language":"tsx","content":"export default function App() {}"}],"runInstructions":[],"caveats":[]}
  \`\`\``);
  assert.equal(result.files[0]?.path, "src/App.tsx");
});

test("writes generated project files inside the requested output directory", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-to-code-output-"));
  const output = path.join(directory, "generated", "qwen");
  const project = generatedProjectSchema.parse({
    summary: "Page",
    assumptions: [],
    files: [
      { path: "src/App.tsx", language: "tsx", content: "export default function App() {}\n" },
      { path: "package.json", language: "json", content: "{}\n" },
    ],
    runInstructions: ["pnpm install", "pnpm dev"],
    caveats: [],
  });

  const written = await writeGeneratedProject(project, output);

  assert.equal(written.outputDirectory, path.resolve(output));
  assert.equal(written.files.length, 2);
  assert.equal(await readFile(path.join(output, "src/App.tsx"), "utf8"), "export default function App() {}\n");
  assert.equal(await readFile(path.join(output, "package.json"), "utf8"), "{}\n");
});

test("rejects duplicate generated file paths", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "image-to-code-output-"));
  const project = generatedProjectSchema.parse({
    summary: "Page",
    assumptions: [],
    files: [
      { path: "src/App.tsx", language: "tsx", content: "first" },
      { path: "src/App.tsx", language: "tsx", content: "second" },
    ],
    runInstructions: [],
    caveats: [],
  });

  await assert.rejects(writeGeneratedProject(project, directory), /duplicate path/);
});

test("resolves relative output paths from the pnpm workspace root", async () => {
  const workspace = await mkdtemp(path.join(os.tmpdir(), "image-to-code-workspace-"));
  const packageDirectory = path.join(workspace, "agents", "example");
  await mkdir(packageDirectory, { recursive: true });
  await writeFile(path.join(workspace, "pnpm-workspace.yaml"), "packages: []\n");

  const resolved = await resolveProjectOutputDirectory("./generated/glm", packageDirectory);

  assert.equal(resolved, path.join(workspace, "generated", "glm"));
});
