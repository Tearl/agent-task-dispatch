import { z } from "zod";

export const imageRequestSchema = z.object({
  prompt: z.string().trim().min(1).max(1_000),
  size: z.enum([
    "1280x1280", "1568x1056", "1056x1568", "1472x1088",
    "1088x1472", "1728x960", "960x1728",
  ]).default("1280x1280"),
  quality: z.literal("hd").default("hd"),
}).strict();

export type ImageRequest = z.infer<typeof imageRequestSchema>;

export interface GeneratedImage {
  prompt: string;
  mimeType: "image/png" | "image/jpeg" | "image/webp";
  size: ImageRequest["size"];
  quality: ImageRequest["quality"];
  bytes: number;
  sha256: string;
  imageUrl: string;
}
