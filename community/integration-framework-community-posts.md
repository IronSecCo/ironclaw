# Framework-community posts: sandbox-your-agent (IRO-348)

Ready-to-fire copy for posting **inside each framework's own community** (Discord,
Discourse forum, subreddit, GitHub Discussions) where developers already ask "how
do I sandbox my agent". This is community-native, help-first copy, distinct from
the launch-traffic posts in `integration-guides-launch-posts.md` (IRO-297).

Docs targets (SEO cluster, IRO-348):

- Hub: `docs/integrations/index.md` (Sandbox any AI agent framework)
- `docs/integrations/langchain.md` (Sandbox your LangChain agent)
- `docs/integrations/crewai.md` (Sandbox your CrewAI agents)
- `docs/integrations/openai-sdk.md` (Sandbox your OpenAI Agents SDK agent)
- `docs/integrations/claude-sdk.md` (Sandbox your Claude Agent SDK agent)

Runnable examples land with IRO-346 (LangChain, CrewAI) and IRO-347 (OpenAI Agents
SDK, Claude Agent SDK) under `examples/integrations/`.

**Status: DRAFT for board to send. Not launch-gated (help-first replies in the
frameworks' own channels are ordinary community participation), but confirm the
board is comfortable posting under a company identity before sending.**

Guardrails: no owner name, no personal handles. No em-dashes or en-dashes
(public-copy standing rule, IRO-254). Every claim maps to a shipped, docs-linked
capability. Post as a helpful answer, not a drive-by ad: lead with the problem the
reader has, link the specific page, never spam the same text across threads.

Replace `<DOCS>` with the published docs base URL and `<REPO>` with
`https://github.com/IronSecCo/ironclaw` at post time.

---

## LangChain (Discord #tools / forum / r/LangChain)

**When to post:** a thread asking how to run `ShellTool` / code execution safely,
how to sandbox an agent, or worrying about prompt injection reaching the host.

> Running the agent's tools in your own process is the part that bites you: an
> `AgentExecutor` runs `ShellTool` with your privileges, your API key sits in the
> process, and egress is wide open, so one prompt-injected instruction is a shell on
> your box.
>
> One option is to move the tool execution into a sealed sandbox instead of wrapping
> the LangChain process. We wrote up the mapping (LangChain agent to a sandboxed
> agent, field by field: model, key, tools, executor) here: <DOCS>/integrations/langchain/
>
> The short version: the model key stays host-side and never enters the sandbox, the
> sandbox runs with no network card by default, and privileged tool calls go through
> an approval gateway. There is a credential-free demo on that page you can run in a
> minute with just Docker, and a runnable LangChain example under
> `examples/integrations/langchain`. Open source, AGPLv3.

---

## CrewAI (Discord / community forum / r/crewai)

**When to post:** a thread about multi-agent safety, crew members running shell or
code tools, or agents handing off untrusted output to each other.

> Multi-agent is where the blast radius grows: every crew member holds the keys,
> every tool runs with your privileges, and hand-offs happen in-process with no
> boundary, so one poisoned task can travel the whole crew.
>
> We put a boundary at each seam by running each crew member in its own sealed,
> per-session sandbox: keys held host-side, agent-to-agent messaging host-mediated
> and audited (no peer-to-peer), and spawning a new agent gated on human approval.
> Mapping from a CrewAI crew, member by member: <DOCS>/integrations/crewai/
>
> Credential-free demo on the page (Docker only), and a runnable CrewAI example
> under `examples/integrations/crewai`. Open source, AGPLv3.

---

## OpenAI Agents SDK (community.openai.com / Discord / r/OpenAI)

**When to post:** a thread about the Agents SDK or function calling running tools
locally, sandboxing tool execution, or keeping the API key out of tool code.

> With the Agents SDK the familiar loop is also the exposure: the client holds your
> API key in the process, your tool functions run with full local privileges, and
> the process can reach any host. One tool call the model was talked into making,
> and that is your box.
>
> IronClaw speaks the OpenAI wire format, so moving the tool execution into a sealed
> sandbox is a short trip: the key stays host-side and never enters the sandbox, the
> sandbox has no network card by default, and there is no `run_shell` that is your
> shell (built-in tools touch only the agent's private workspace, anything stronger
> is a reviewed tool). Mapping here: <DOCS>/integrations/openai-sdk/
>
> Credential-free demo on the page, runnable example under
> `examples/integrations/openai-agents`. Open source, AGPLv3.

---

## Claude Agent SDK (Anthropic Discord / community / r/ClaudeAI)

**When to post:** a thread about the `Bash` tool or code execution running on the
host, sandboxing the Claude Agent SDK, or prompt injection reaching the shell.

> The `Bash` tool is exactly what makes the Claude Agent SDK useful and exactly what
> makes it dangerous on your host: the model chooses the command, a poisoned document
> can choose the model, and the tool runs with your privileges. The API key is in the
> process and egress is open too.
>
> One fix is to back the bash/code tools with a sealed sandbox session instead of
> running them locally: no host shell to hijack, the key stays host-side, the sandbox
> runs with no network card by default, and any real shell tool is registered once,
> in the open, through an approval gateway. Mapping from a Claude Agent SDK agent:
> <DOCS>/integrations/claude-sdk/
>
> Credential-free demo on the page, runnable example under
> `examples/integrations/claude-agent-sdk`. Open source, AGPLv3.

---

## GitHub Discussions / general "how do I sandbox my agent" threads

Point to the hub, which routes to the right framework page:

> If you are running an agent's tools or generated code on your own machine, the
> host is the trust boundary you did not choose. IronClaw runs the same agent (any of
> LangChain, CrewAI, the OpenAI Agents SDK, or the Claude Agent SDK) inside a sealed
> gVisor sandbox: no network card by default, key held host-side, every privileged
> action gated and audited. Start here, it routes to your framework:
> <DOCS>/integrations/ . Open source, AGPLv3, with a credential-free demo you can run
> in a minute.

---

## Posting discipline (for the board)

- One tailored reply per genuinely relevant thread. Never paste the same block twice.
- Answer the question first; the link is supporting, not the point.
- Disclose the affiliation plainly ("we built ...") when posting in a community.
- Skip threads where a sandbox is off-topic. Credibility over reach.
