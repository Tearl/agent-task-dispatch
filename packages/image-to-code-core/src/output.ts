import { access, mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { generatedProjectSchema, type GeneratedProject } from "./schema.ts";

export type WrittenProject = {
  outputDirectory: string;
  files: string[];
};

export async function resolveProjectOutputDirectory(
  outputDirectory: string,
  startDirectory = process.cwd(),
): Promise<string> {
  if (!outputDirectory.trim()) throw new Error("output directory must not be empty");
  if (path.isAbsolute(outputDirectory)) return path.resolve(outputDirectory);

  let current = path.resolve(startDirectory);
  while (true) {
    try {
      await access(path.join(current, "pnpm-workspace.yaml"));
      return path.resolve(current, outputDirectory);
    } catch {
      const parent = path.dirname(current);
      if (parent === current) return path.resolve(startDirectory, outputDirectory);
      current = parent;
    }
  }
}

export async function writeGeneratedProject(
  project: GeneratedProject,
  outputDirectory: string,
): Promise<WrittenProject> {
  if (!outputDirectory.trim()) throw new Error("output directory must not be empty");

  const validatedProject = generatedProjectSchema.parse(project);
  const root = path.resolve(outputDirectory);
  const writtenFiles: string[] = [];
  const seenPaths = new Set<string>();
  const plannedFiles = validatedProject.files.map((file) => {
    const normalizedPath = file.path.replaceAll("\\", "/");
    if (seenPaths.has(normalizedPath)) {
      throw new Error(`generated project contains duplicate path: ${file.path}`);
    }
    seenPaths.add(normalizedPath);

    const destination = path.resolve(root, normalizedPath);
    if (destination !== root && !destination.startsWith(`${root}${path.sep}`)) {
      throw new Error(`generated file path escapes output directory: ${file.path}`);
    }

    return { file, destination };
  });

  await mkdir(root, { recursive: true });

  for (const { file, destination } of plannedFiles) {
    await mkdir(path.dirname(destination), { recursive: true });
    await writeFile(destination, file.content, "utf8");
    writtenFiles.push(destination);
  }

  return { outputDirectory: root, files: writtenFiles };
}
