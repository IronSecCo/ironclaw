# PR Description Drafter

An agent that reads commit messages or a diff and drafts a clear, descriptive Pull Request summary. 

## Run it credential-free (mock provider, no keys)

The zero-credential demo control-plane seeds an offline `mock-agent` whose `mock`
provider makes **no network call and needs no model key**, so the whole pipeline
(inbound → sandbox loop → provider → reply) runs on a stock laptop. From the repo
root:

```sh
docker compose -f docker-compose.demo.yml up -d --build   # one-time: build + start
./examples/pr-description-drafter/run-mock.sh             # drive the recipe
```

`run-mock.sh` asks the agent to draft a PR description from a set of dummy commits.
With the `mock` provider the output is an echo that proves the round-trip; swap in
a real model (set `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` on the control-plane) and
the *same* wiring produces a real PR draft.

Tear down with `docker compose -f docker-compose.demo.yml down`.

## What to notice

- **Zero plumbing.** The agent just needs the diff/commits as context; it leverages its system prompt to output a structured PR description.
- **The model is swappable.** `mock` proves the plumbing credential-free; a real
  provider drafts the actual description with zero wiring changes.
