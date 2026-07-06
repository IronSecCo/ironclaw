/**
 * Vercel AI SDK tool backed by an IronClaw sandbox.
 *
 * Drop-in replacement for a host-executing shell/code tool: the agent calls it
 * exactly the same way, but every command runs inside an isolated IronClaw
 * per-session sandbox instead of on your machine.
 *
 *     import { IronClawSandbox } from "../_shared/ironclaw-sandbox";
 *     import { makeSandboxTool } from "./ironclaw-tool";
 *
 *     const sandbox = await new IronClawSandbox().engage();
 *     const tools = { sandboxedShell: makeSandboxTool(sandbox) };
 *     await generateText({ model, tools, prompt });  // ... plug into any agent
 */

import { tool, type Tool } from "ai";
import { z } from "zod";

import type { IronClawSandbox } from "../_shared/ironclaw-sandbox";

const DESCRIPTION =
  "Execute a shell command inside an isolated IronClaw sandbox and return its " +
  "stdout/stderr and exit code. Use this for any code the user asks you to run. " +
  "The sandbox has no network, no access to the host filesystem, and no Docker " +
  "socket, so it is safe to run untrusted commands.";

/** Wrap a live IronClaw sandbox as a Vercel AI SDK tool. */
export function makeSandboxTool(sandbox: IronClawSandbox): Tool {
  return tool({
    description: DESCRIPTION,
    inputSchema: z.object({
      command: z.string().describe("The shell command to run inside the sandbox."),
    }),
    execute: async ({ command }): Promise<string> => {
      const result = await sandbox.run(command);
      return result.toString();
    },
  });
}
