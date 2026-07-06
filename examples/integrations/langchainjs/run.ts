/**
 * Run a LangChain.js agent whose code execution is backed by an IronClaw sandbox.
 *
 * Zero credentials by default: it engages a real IronClaw per-session sandbox
 * against the offline demo control-plane (mock provider) and drives the
 * LangChain `sandboxed_shell` tool exactly as an agent would -- one benign task
 * plus a battery of escape attempts -- then prints a PASS/FAIL containment table.
 *
 * Set OPENAI_API_KEY to instead let a real LLM-driven ReAct agent decide what to
 * run; the tool -- and therefore the isolation -- is identical either way.
 */

import type { DynamicStructuredTool } from "@langchain/core/tools";
import { createReactAgent } from "@langchain/langgraph/prebuilt";
import { ChatOpenAI } from "@langchain/openai";

import { PROBES, runContainmentDemo } from "../_shared/containment-demo";
import { IronClawSandbox } from "../_shared/ironclaw-sandbox";
import { makeSandboxTool } from "./ironclaw-tool";

/** Optional: let a real LLM-driven ReAct agent decide what to run (needs a key). */
async function driveWithRealAgent(tool: DynamicStructuredTool): Promise<number> {
  const llm = new ChatOpenAI({ model: "gpt-4o-mini", temperature: 0 });
  const agent = createReactAgent({ llm, tools: [tool] });
  const res = await agent.invoke({
    messages: [
      { role: "user", content: "Run `id` and tell me which user the sandbox runs as." },
    ],
  });
  const last = res.messages[res.messages.length - 1];
  console.log(typeof last?.content === "string" ? last.content : JSON.stringify(last?.content));
  return 0;
}

async function main(): Promise<number> {
  console.log("==> engaging an IronClaw per-session sandbox (mock provider, zero-cred)");
  const sandbox = await new IronClawSandbox().engage();
  console.log(`    sandbox container: ${sandbox.container}`);
  const tool = makeSandboxTool(sandbox);

  if (process.env.OPENAI_API_KEY) {
    console.log("==> OPENAI_API_KEY set: driving a real ReAct agent");
    return driveWithRealAgent(tool);
  }

  // Deterministic, zero-cred path: call the tool exactly as an agent does --
  // tool.invoke({ command }) -- for each probe.
  console.log(`==> driving the '${tool.name}' tool over ${PROBES.length} agent commands`);
  return runContainmentDemo(
    (command) => tool.invoke({ command }) as Promise<string>,
    "LangChain.js",
  );
}

main().then(
  (code) => process.exit(code),
  (err) => {
    console.error(err);
    process.exit(1);
  },
);
