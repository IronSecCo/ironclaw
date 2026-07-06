/**
 * Run a Vercel AI SDK agent whose code execution is backed by an IronClaw sandbox.
 *
 * Zero credentials by default: it engages a real IronClaw per-session sandbox
 * against the offline demo control-plane (mock provider) and drives the
 * `sandboxedShell` tool exactly as an agent would -- one benign task plus a
 * battery of escape attempts -- then prints a PASS/FAIL containment table.
 *
 * Set OPENAI_API_KEY to instead let a real LLM-driven agent decide what to run;
 * the tool -- and therefore the isolation -- is identical either way.
 */

import { generateText, stepCountIs, type Tool } from "ai";
import { openai } from "@ai-sdk/openai";

import { PROBES, runContainmentDemo } from "../_shared/containment-demo";
import { IronClawSandbox } from "../_shared/ironclaw-sandbox";
import { makeSandboxTool } from "./ironclaw-tool";

/** Optional: let a real LLM-driven agent decide what to run (needs a key). */
async function driveWithRealAgent(sandboxTool: Tool): Promise<number> {
  const { text } = await generateText({
    model: openai("gpt-4o-mini"),
    tools: { sandboxedShell: sandboxTool },
    stopWhen: stepCountIs(5),
    prompt: "Run `id` and tell me which user the sandbox runs as.",
  });
  console.log(text);
  return 0;
}

async function main(): Promise<number> {
  console.log("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)");
  const sandbox = await new IronClawSandbox().engage();
  console.log(`    sandbox container: ${sandbox.container}`);
  const sandboxTool = makeSandboxTool(sandbox);

  if (process.env.OPENAI_API_KEY) {
    console.log("==> OPENAI_API_KEY set: driving a real Vercel AI SDK agent");
    return driveWithRealAgent(sandboxTool);
  }

  // Deterministic, zero-cred path: call the tool's `execute` exactly as the AI
  // SDK's agent loop does -- execute({ command }, options) -- for each probe.
  console.log(`==> driving the 'sandboxedShell' tool over ${PROBES.length} agent commands`);
  const execute = sandboxTool.execute;
  if (!execute) throw new Error("sandbox tool has no execute()"); // never: we set it
  return runContainmentDemo(
    (command) =>
      execute(
        { command },
        { toolCallId: "containment-probe", messages: [], context: undefined },
      ) as Promise<string>,
    "Vercel AI SDK",
  );
}

main().then(
  (code) => process.exit(code),
  (err) => {
    console.error(err);
    process.exit(1);
  },
);
