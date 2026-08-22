import { z } from "zod";

export const generatedProjectSchema = z.object({
  summary: z.string().describe("A concise description of the reconstructed interface"),
  assumptions: z.array(z.string()).describe("Visual or product assumptions made because the screenshot was ambiguous"),
  files: z.array(z.object({
    path: z.string().refine(isSafeProjectPath, "path must stay inside the generated project").describe("Safe project-relative file path"),
    language: z.string().describe("Code fence language, such as tsx, css, or json"),
    content: z.string().describe("Complete file contents without a Markdown code fence"),
  })).min(1),
  runInstructions: z.array(z.string()).describe("Commands or steps required to run the generated code"),
  caveats: z.array(z.string()).describe("Known gaps such as unavailable fonts, assets, or interactions"),
});

export type GeneratedProject = z.infer<typeof generatedProjectSchema>;

export function parseGeneratedProjectText(value: string): GeneratedProject {
  const trimmed = value.trim().replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/, "");
  const start = trimmed.indexOf("{");
  const end = trimmed.lastIndexOf("}");
  if (start < 0 || end < start) throw new Error("model response did not contain a JSON object");

  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed.slice(start, end + 1));
  } catch (error) {
    throw new Error("model response contained invalid JSON", { cause: error });
  }
  return generatedProjectSchema.parse(parsed);
}

function isSafeProjectPath(value: string): boolean {
  if (!value || value.includes("\0") || value.startsWith("/") || value.startsWith("\\")) return false;
  if (/^[a-zA-Z]:[\\/]/.test(value)) return false;
  const segments = value.replaceAll("\\", "/").split("/");
  return segments.every((segment) => segment !== "" && segment !== "." && segment !== "..");
}
