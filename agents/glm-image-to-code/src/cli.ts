import {
  loadImageInput,
  resolveProjectOutputDirectory,
  writeGeneratedProject,
} from "@agent-platform/image-to-code-core";
import { generateCodeFromImage } from "./generate.ts";

const args = process.argv.slice(2);
const imagePath = positionalValue(args);
if (!imagePath || args.includes("--help")) {
  process.stderr.write("Usage: pnpm generate <image> [--target=stack] [--prompt=instructions] [--out=directory]\n");
  process.exit(imagePath ? 0 : 1);
}

const image = await loadImageInput(imagePath);
const result = await generateCodeFromImage({
  image,
  target: optionValue(args, "--target"),
  prompt: optionValue(args, "--prompt"),
});
const outputDirectory = optionValue(args, "--out");
if (outputDirectory !== undefined) {
  const written = await writeGeneratedProject(
    result,
    await resolveProjectOutputDirectory(outputDirectory),
  );
  process.stdout.write(`Generated ${written.files.length} files in ${written.outputDirectory}\n`);
  for (const file of written.files) process.stdout.write(`- ${file}\n`);
  if (result.runInstructions.length) {
    process.stdout.write(`Run instructions:\n${result.runInstructions.map((step) => `- ${step}`).join("\n")}\n`);
  }
} else {
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
}

function optionValue(values: string[], name: string): string | undefined {
  const prefix = `${name}=`;
  const inline = values.find((value) => value.startsWith(prefix));
  if (inline) return inline.slice(prefix.length);
  const index = values.indexOf(name);
  return index >= 0 ? values[index + 1] : undefined;
}

function positionalValue(values: string[]): string | undefined {
  for (let index = 0; index < values.length; index += 1) {
    const value = values[index];
    if (!value || value === "--") continue;
    if (["--target", "--prompt", "--out"].includes(value)) {
      index += 1;
      continue;
    }
    if (!value.startsWith("--")) return value;
  }
  return undefined;
}
