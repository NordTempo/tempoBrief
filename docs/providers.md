# Model providers

tempoBrief makes three kinds of structured calls: scoring batches (most calls,
effort `low`), the duplicate check before each brief, and the policy rewrite
(effort `medium`). Every call goes through `llm.Generate[T]`: the Go answer type
becomes a strict JSON Schema (all properties required, no extra properties) and
the answer is decoded with unknown fields rejected.

| Kind | Setting | Auth | Notes |
|---|---|---|---|
| `anthropic` | Model (default `claude-haiku-5-5`) | API key in Settings, else `ANTHROPIC_API_KEY` or an `ant auth login` profile | Official Go SDK, structured outputs, prompt caching on the system prompt |
| `openai` | Model (required), base URL | API key, optional for local servers | Chat completions with `response_format: json_schema`. Works with OpenAI, OpenRouter, Ollama, LM Studio, vLLM |
| `claude-cli` | Model (default `haiku`) | Whatever `claude` is logged in with (`claude setup-token`, `CLAUDE_CODE_OAUTH_TOKEN`) | `claude -p --json-schema`, tools off, no session persistence |
| `codex-cli` | Model (CLI default) | Whatever `codex` is logged in with | `codex exec --output-schema`, read-only sandbox |

The CLI kinds need the CLI on `PATH`: use the `-cli` image, or run the binary
on a machine where the CLI is installed. tempoBrief never sees or stores
subscription credentials; it only runs the CLI the operator has logged in. Each
subscription's terms decide whether running its CLI unattended is allowed;
check them before relying on it.

"Test" in Settings makes one tiny call and reports the model and latency. The
setup checklist counts the provider as working once a call succeeded with the
current provider and model.

**OpenRouter** (base URL `https://openrouter.ai/api/v1`): requests ask for
`provider.require_parameters`, so OpenRouter only routes to hosts that honour
the JSON schema. Free models (IDs ending in `:free`) allow 20 requests a minute
and 50 a day (1000 after buying 10 credits once), enough for the volume below.
Pick one whose `supported_parameters` include `structured_outputs`.

Rough volume for 40 feeds: 300 to 600 new stories a day, so 10 to 15 scoring
calls, one or two duplicate checks per brief, and a policy rewrite every few
days.
