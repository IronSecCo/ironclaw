# IronClaw Pitch — Speaker Notes

Open `pitch/index.html` in a browser and press <kbd>F</kbd> for fullscreen. Navigate with the arrow
keys or space. Press <kbd>S</kbd> to show these notes on the slide while you rehearse. The same notes
are embedded in the deck, so this file is the script you can print or read on a second screen.

Total target: **about 6 minutes** (inside the 5 to 7 minute window). Plain, founder voice. Topaz
leads; Omer takes the security beats on slides 5 and 8 if you want two voices.

---

## Timing at a glance

| # | Slide | Time | Who |
|---|---|---:|---|
| 1 | Who we are (team) | 0:35 | Topaz |
| 2 | IronClaw (reveal) | 0:15 | Topaz |
| 3 | The issue | 0:50 | Topaz |
| 4 | The diagnosis (why now) | 0:45 | Topaz |
| 5 | Product (how we addressed it) | 1:10 | Omer |
| 6 | Competitors | 0:55 | Topaz |
| 7 | Market + wedge | 0:55 | Topaz |
| 8 | What we built in 48 hours | 1:10 | Omer (live demo) |
| 9 | What's in it for you (close) | 0:35 | Topaz |
| | **Total** | **~6:10** | |

The flow is deliberate: **issue (3) → diagnosis (4) → how we addressed it (5)**, then proof (6 to 8),
then the FOMO close (9). We do not end on a money ask.

---

## Slide 1 — Who we are / team (~35s) — this is the opener

"Hi, quick on us. I'm Topaz, product and go-to-market, I just finished my MBA at MIT Sloan, with a
background from the Technion, Intel, IBM and AWS. Omer leads security and architecture, from the
Intelligence Corps into data architecture and cybersecurity."

"We worked together for years in Tel Aviv, then reconnected as co-founders to build a secure AI
agents platform for enterprises. We actually came here as mentors. On day two we decided to build
something for the developer community too. Forty-eight hours later, this is IronClaw."

> Open warm. The logos do the credibility work, do not narrate every one. Land "worked together in
> Tel Aviv, reconnected as co-founders, came as mentors, built this in 48 hours," then hand to the
> product reveal.

## Slide 2 — IronClaw / reveal (~15s)

"And here is what that turned into. IronClaw. AI agents you can actually trust. A secure developer
control plane for the OpenClaw-style agents everyone is starting to build."

> This is the product reveal right after the intro, so let the logo land. Name the product and the
> one-line promise, then move straight into the problem.

## Slide 3 — The issue (~50s)

"Here's the problem we kept running into. An agent only earns its keep if it can act on its own. But
the moment it can act, it can also do harm. And today those actions land in the most sensitive parts
of your stack: your repos, your terminal, your secrets, your chat channels, your APIs, and memory
that lasts."

"Now the part people miss. The standard answer to all of this is 'don't worry, we keep audit logs.'
But think it through. If the agent can change its own code, it can change how it writes those logs.
An agent auditing itself is not an audit. Once you see that, you realize the whole trust model has to
sit outside the agent."

> This is the "issue" beat, and the audit insight is the hook. Let the access chips pile up, then
> deliver the audit-trap line slowly. It sets up slide 5, where the sealed runtime is exactly what
> moves trust outside the agent.

## Slide 4 — The diagnosis / why now (~45s)

"And the demand is not theoretical. AI spend is going from three hundred billion to over six hundred
billion in three years, per IDC. Eighty-four percent of developers already use or plan to use AI
tools, per Stack Overflow. And on GitHub, over a million public repos already have an LLM wired in."

"So adoption is solved. The missing piece is trust. Nobody owns the security layer yet. That's the
opening."

> The source logo now sits next to each number (IDC, Stack Overflow, GitHub), so name the source
> naturally. These are research snapshots. **Verify before any live or fundraising use.** If a judge
> presses, say exactly that.

## Slide 5 — Product / how we addressed it (~70s)

"So here is what we built. IronClaw is a secure control plane for agents. Four walls, all of them
things you can check, not promises."

"Sealed runtime: it ships as compiled Go, so the agent can't rewrite itself, which also means it
can't forge its own audit trail, the exact problem from the last slide. Sealed sandbox: gVisor with
no network at all, the model only reachable through our host proxy. Encrypted queues: every session
has its own keys and the inbox is read-only."

"And the one I want you to remember. The approval gateway is not a speed bump on every action. The
agent works on its own. The only thing that stops for a human is a change to the agent's own powers.
It can ask for more access. It can never grant itself more access. The trust sits outside the agent,
where it cannot reach it."

> **Most important slide.** Do not let the gateway be misheard as "every action needs approval."
> Tie the sealed runtime back to the audit-trap from slide 3: that is what makes the trust trustworthy.

## Slide 6 — Competitors (~55s)

"We are not alone, and that's good news. OpenClaw has nearly four hundred thousand stars. That tells
you the demand is real. Hermes is rich and capable. NanoClaw made isolation a feature. LangGraph owns
orchestration. OpenHands proved developers want coding agents."

"But look at the last column. In every one of them security is either not the story, or it depends on
how you happen to deploy. We're not trying to beat OpenClaw on community. We're building the one a
security team can actually approve. OpenClaw proved demand, NanoClaw proved isolation matters, we make
security the default."

> Star and community counts are research snapshots. Verify before going live. Do not claim to beat
> anyone on breadth; claim the security-default lane.

## Slide 7 — Market + wedge (~55s)

"One thing to be clear about. We are not another coding assistant competing with Cursor or Copilot.
We are the secured layer their agents run inside. And the market math is a clean funnel. The
agentic-AI market is heading to over fifty billion dollars by 2030, growing about forty-six percent a
year. The slice we directly serve, the secure runtime every developer adopting an agent needs, is
around nine and a half billion. And realistically, in three years, we win on the order of forty
million in ARR, through design partners and the open-source wedge converting to enterprise governance.
Those seat prices just show the market already pays for agent tooling; we price the control plane
beneath it."

"The motion is simple. We win the individual developer because it's open source and useful in five
minutes. Then the same architecture, the sandbox and the gateway, is exactly what a security team
needs to sign off. We win the company by making their review easy."

> The reframe matters: IronClaw is infrastructure beneath the agents, not another assistant. The
> Cursor/Copilot prices are evidence of willingness to pay, not competitors.

## Slide 8 — What we built in 48 hours (~70s, live if the room allows)

"This is not slideware. It's open source, it's on GitHub, and it runs. One command installs it, with
a checksum check, and the control-plane comes up in dev mode."

"And this is the part I care about. The security here is not a human clicking approve on everything.
It is enforced by the design, whether the agent behaves or not. The agent ships as a sealed Go binary,
so it can't rewrite itself or fake its own logs. The sandbox has no network, so even a hostile agent
has nowhere to send your data. Every session is encrypted with its own key. The approval gate is just
the last line: the agent can ask for more power, but it physically cannot grant it to itself."

"Under the hood: two Go binaries that never share memory, around two hundred tests green, six chat
channels, MIT licensed. We're proving the wedge, not the whole enterprise suite. And because it's open
source, the real proof is that you can clone it and try to break it tonight."

> Reframe away from "human in the loop slows it down." Lead with the always-on structural guarantees
> in the panel; the gateway is the last line, not the headline. If you can, still run the live demo:
> `ironclaw-controlplane --dev`, then `ironctl change submit` → `change pending` → `change approve`.

## Slide 9 — Spread the word / close (~35s + close)

"One last thing, and it is the real ask. We are not here for a check today. We want IronClaw in front
of the people who need it. ClawComp, showcase it: it was built at your event, feature it in the next
competitions. Builder and AI-first teams, adopt it: pull the suite into your products so your agents
get real access and ship secure from day one. Investors, recommend it: if you want your portfolio to
go AI-first without going exposed, send them the project. And developers, contribute to it: it is open
source, send a PR, harden a wall, and make it stronger for everyone."

"It is open source and it runs today. If you want to go deeper, including on the investment side, find
us right after."

> The whole job of this slide is exposure, not a money ask. Give each group one clear verb: showcase,
> adopt, recommend, contribute. The developer ask matters: open-source contributions are what compound
> IronClaw's security stance over time. Land "IronClaw is the next big thing in secure agents, help us
> spread it." Keep the investment conversation off the slide and offer it as a follow-up.

---

## 30-second closing script

> "Two of us, 48 hours, one idea: treat the agent as untrusted and put the trust outside it, where it
> can't reach. Compiled Go so it can't rewrite itself or fake its own logs. A gVisor sandbox with no
> network. Encrypted per-session queues. And a gateway where the agent can ask for more power but can
> never grant it to itself.
>
> It's open source and it runs today. The market already wants persistent developer agents. The
> blocker is trust. IronClaw removes that blocker, and it is the next big thing in secure agents.
> Help us spread the word. Thank you."

---

## Narrative arc (one line each)

1. Two builders who kept seeing the same gap and became co-founders, built this in 48 hours.
2. The reveal: IronClaw, agents you can actually trust.
3. The more useful an agent is, the more dangerous it is, and an agent that audits itself can't be trusted.
4. Demand is proven; trust is the missing layer.
5. We put the trust outside the agent: four walls, and only self-power-changes need a human.
6. Everyone proves demand; nobody owns secure-by-default.
7. We are the secured runtime agents run inside, not another assistant.
8. It is real, open source, and the security is enforced by design, not by good behavior.
9. Spread the word: showcase, adopt, recommend, contribute. IronClaw is the next big thing in secure agents.

## Q&A landmines (be precise)

- **"Does every action need approval?"** No. The agent is autonomous inside the sandbox. Only changes
  to its own capabilities/harness are held at the gateway. Everything else is enforced structurally.
- **"How is your audit trail trustworthy if the agent runs the show?"** It isn't the agent's to edit.
  The agent is a sealed binary with no network; the record is written by the host, outside the agent.
- **"Are those market and competitor numbers current?"** They are research snapshots; we verify before
  any live or fundraising use. Do not present them as audited.
- **"Aren't you competing with Cursor / Copilot?"** No. We are the secured layer their agents run
  inside. Their pricing is evidence the market pays for agent tooling, not a competitive set.
- **"Is the GitHub-issue-to-patch flow built?"** Not yet. The gateway, sandbox, and encrypted queues
  are built and runnable today. The issue-to-patch workflow is the next step on top of them.
- **"So what are you actually asking for?"** Adoption first: teams building on it, ClawComp amplifying
  it, investors pointing portfolio companies at it. The funding conversation happens after, off-stage.
