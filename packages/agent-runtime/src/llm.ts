export interface JsonGenerator {
  generate(
    systemPrompt: string,
    userPrompt: string,
    signal?: AbortSignal,
    options?: JsonGenerationOptions,
  ): Promise<unknown>;
}

export interface JsonGenerationMetadata {
  responseId?: string;
  model?: string;
  rawContent: string;
}

export interface JsonGenerationOptions {
  jsonSchema?: {
    name: string;
    schema: Record<string, unknown>;
    strict?: boolean;
  };
  onResponse?: (metadata: JsonGenerationMetadata) => void;
}

export interface CompatibleChatProviderOptions {
  baseUrl: string;
  model: string;
  apiKey?: string;
  timeoutMs?: number;
  temperature?: number;
  maxTokens?: number;
}

interface ChatCompletionResponse {
  id?: string;
  model?: string;
  choices?: Array<{ message?: { content?: string } }>;
}

export class CompatibleChatProvider implements JsonGenerator {
  private readonly options: CompatibleChatProviderOptions;

  constructor(options: CompatibleChatProviderOptions) {
    this.options = options;
  }

  async generate(
    systemPrompt: string,
    userPrompt: string,
    signal?: AbortSignal,
    generationOptions?: JsonGenerationOptions,
  ): Promise<unknown> {
    if (!this.options.model) throw new Error("LLM_MODEL is required");
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (this.options.apiKey) headers.Authorization = `Bearer ${this.options.apiKey}`;

    let response: Response;
    try {
      response = await fetch(`${this.options.baseUrl.replace(/\/$/, "")}/chat/completions`, {
        method: "POST",
        headers,
        body: JSON.stringify({
          model: this.options.model,
          temperature: this.options.temperature ?? 0.1,
          ...(this.options.maxTokens ? { max_tokens: this.options.maxTokens } : {}),
          response_format: generationOptions?.jsonSchema
            ? {
                type: "json_schema",
                json_schema: {
                  name: generationOptions.jsonSchema.name,
                  strict: generationOptions.jsonSchema.strict ?? true,
                  schema: generationOptions.jsonSchema.schema,
                },
              }
            : { type: "json_object" },
          messages: [
            { role: "system", content: systemPrompt },
            { role: "user", content: userPrompt },
          ],
        }),
        signal: combinedSignal(signal, this.options.timeoutMs ?? 120_000),
      });
    } catch (error) {
      throw new Error(`LLM request failed: ${errorMessage(error)}`, { cause: error });
    }
    if (!response.ok) throw new Error(`LLM provider returned ${response.status}`);

    const payload = await response.json() as ChatCompletionResponse;
    const content = payload.choices?.[0]?.message?.content;
    if (!content) throw new Error("LLM provider returned empty content");
    generationOptions?.onResponse?.({
      responseId: payload.id,
      model: payload.model,
      rawContent: content,
    });
    try {
      return JSON.parse(stripCodeFence(content));
    } catch {
      throw new Error("LLM provider returned invalid JSON");
    }
  }
}

function stripCodeFence(value: string): string {
  return value.trim().replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/, "");
}

function errorMessage(error: unknown): string {
  const messages: string[] = [];
  let current: unknown = error;
  for (let depth = 0; depth < 3 && current; depth += 1) {
    if (!(current instanceof Error)) break;
    const code = "code" in current && typeof current.code === "string" ? ` (${current.code})` : "";
    const message = `${current.message}${code}`;
    if (!messages.includes(message)) messages.push(message);
    current = current.cause;
  }
  return messages.join(" <- ") || "unknown network error";
}

function combinedSignal(signal: AbortSignal | undefined, timeoutMs: number): AbortSignal {
  const timeout = AbortSignal.timeout(timeoutMs);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}
