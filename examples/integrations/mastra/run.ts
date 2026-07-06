/**
 * Run a Mastra agent whose code execution is backed by an IronClaw sandbox.
 *
 * Zero credentials by default: it engages a real IronClaw per-session sandbox
 * against the offline demo control-plane (mock provider) and drives the
 * `sandboxedShell` tool exactly as an agent would -- one benign task plus a
 * battery of escape attempts -- then prints a PASS/FAIL containment table.
 *
 * Set OPENAI_API_KEY to instead let a real LLM-driven Mastra agent decide what
 * to run; the tool -- and therefore the isolation -- is identical either way.
 */

import { Agent } from "@mastra/core/agent";
import { openai } from "@ai-sdk/openai";

import { PROBES, runContainmentDemo } from "../_shared/containment-demo";
import { IronClawSandbox } from "../_shared/ironclaw-sandbox";
import { makeSandboxTool } from "./ironclaw-tool";

type SandboxTool = ReturnType<typeof makeSandboxTool>;

/** Optional: let a real LLM-driven Mastra agent decide what to run (needs a key). */
async function driveWithRealAgent(sandboxTool: SandboxTool): Promise<number> {
  const agent = new Agent({
    id: "ironclaw-sandbox-agent",
    name: "ironclaw-sandbox-agent",
    instructions:
      "You have a `sandboxedShell` tool. Answer the user by running commands with it.",
    model: openai("gpt-4o-mini"),
    tools: { sandboxedShell: sandboxTool },
  });
  const result = await agent.generate(
    "Run `id` and tell me which user the sandbox runs as.",
  );
  console.log(result.text);
  return 0;
}

async function main(): Promise<number> {
  console.log("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)");
  const sandbox = await new IronClawSandbox().engage();
  console.log(`    sandbox container: ${sandbox.container}`);
  const sandboxTool = makeSandboxTool(sandbox);

  if (process.env.OPENAI_API_KEY) {
    console.log("==> OPENAI_API_KEY set: driving a real Mastra agent");
    return driveWithRealAgent(sandboxTool);
  }

  // Deterministic, zero-cred path: call the tool's `execute` exactly as Mastra's
  // agent loop does -- execute(validatedInput, context) -- for each probe.
  console.log(`==> driving the 'sandboxedShell' tool over ${PROBES.length} agent commands`);
  const execute = sandboxTool.execute;
  if (!execute) throw new Error("sandbox tool has no execute()"); // never: we set it
  return runContainmentDemo(
    (command) => execute({ command }, {} as never) as Promise<string>,
    "Mastra",
  );
}

main().then(
  (code) => process.exit(code),
  (err) => {
    console.error(err);
    process.exit(1);
  },
);
