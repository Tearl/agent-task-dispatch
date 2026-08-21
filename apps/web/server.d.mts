import type { Server } from "node:http";

export function resolveBffOrigin(raw?: string): URL;
export function startServer(environment?: NodeJS.ProcessEnv): Promise<Server>;
