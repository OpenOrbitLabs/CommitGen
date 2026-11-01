# commitgen

Write clean commit messages from your staged diff with a **local** or **API-based** LLM.
A single binary with no dependencies. Private by default.

```
$ git add .
$ commitgen

  feat(auth): add refresh token rotation

  - rotate refresh tokens on every use to limit replay attacks
  - store token family id so reuse revokes the whole chain

[c]ommit  [e]dit  [r]egenerate  [q]uit (c):
```

## Features

- **Private by default**: talks to a local [Ollama](https://ollama.com) model out of the box. Your code never leaves your machine.
- **Any provider**: any OpenAI-compatible API (OpenAI, Groq, OpenRouter, LM Studio, llama.cpp, vLLM), plus Anthropic.
- **Secret masking**: API keys, tokens, passwords and private keys are redacted before the diff is sent.
- **Smart diffs**: skips lockfiles, minified files and build output, and truncates big diffs while keeping every file header.
- **Matches your repo**: reads recent commit subjects and the branch name so messages follow your team's style.
- **Git hook**: `commitgen hook install`, then plain `git commit` opens with a drafted message.
- **Multiple candidates**: `-n 3` generates three options and lets you pick one.
- **Configurable**: user config plus per-repo `.commitgen.toml` (scopes, style, language, custom rules).

## Install

```bash
go install github.com/onetwothird/commitgen@latest
```

or build from source:

```bash
git clone https://github.com/onetwothird/commitgen && cd commitgen
go build -o commitgen .        # Windows: go build -o commitgen.exe .
```

Put the binary somewhere on your `PATH`.

## Quick start (local, free, private)

```bash
ollama pull qwen2.5-coder:7b     # one-time
git add -A
commitgen                         # interactive: commit / edit / regenerate
```

## Usage

```
commitgen                 generate + interactive menu
commitgen -c              generate, then open git commit in your editor to review
commitgen -c -y           generate and commit immediately
commitgen -p              print only (for scripts / aliases)
commitgen -n 3            pick from 3 candidates
commitgen -m "closes #42" give the model extra context
commitgen --dry-run       show exactly what would be sent to the model
commitgen --model llama3.2 --url http://localhost:11434/v1

commitgen hook install    draft messages automatically on `git commit`
commitgen hook uninstall
commitgen config init     write a starter config file
commitgen config show     show the effective config
```

Git alias: `git config --global alias.ai '!commitgen -c'`, then `git ai`.

## Using a cloud API

```bash
# OpenAI
export COMMITGEN_BASE_URL=https://api.openai.com/v1
export COMMITGEN_MODEL=gpt-4o-mini
export OPENAI_API_KEY=sk-...

# Groq (fast and cheap)
export COMMITGEN_BASE_URL=https://api.groq.com/openai/v1
export COMMITGEN_MODEL=llama-3.1-8b-instant
export COMMITGEN_API_KEY=gsk_...

# Anthropic
export COMMITGEN_PROVIDER=anthropic
export COMMITGEN_MODEL=claude-haiku-4-5
export ANTHROPIC_API_KEY=sk-ant-...
```

On Windows PowerShell, use `$env:COMMITGEN_MODEL = "gpt-4o-mini"`.

## Configuration

Run `commitgen config init` to create the user config (`commitgen config path` shows where it is).
Put a `.commitgen.toml` in a repo root to override settings for that project:

```toml
style  = "conventional"          # or "simple"
scopes = ["api", "ui", "cli"]
exclude = ["*.snap", "docs/generated/*"]
instructions = "Reference the Jira ticket from the branch name, e.g. PROJ-123."
```

Precedence: defaults → user config → repo `.commitgen.toml` → environment variables → flags.

| Key | Default | Description |
|---|---|---|
| `provider` | `openai` | `openai` (any OpenAI-compatible API) or `anthropic` |
| `base_url` | `http://localhost:11434/v1` | API base URL |
| `model` | `qwen2.5-coder:7b` | Model name |
| `api_key` | — | Prefer env vars: `COMMITGEN_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY` |
| `style` | `conventional` | `conventional` or `simple` |
| `language` | `English` | Language of the message |
| `max_diff` | `12000` | Max diff characters sent |
| `history` | `10` | Recent commit subjects used for style matching |
| `temperature` | `0.2` | Sampling temperature |
| `timeout` | `120` | Request timeout (seconds) |
| `exclude` | — | Extra pathspecs to leave out of the diff |
| `scopes` | — | Allowed Conventional Commit scopes |
| `instructions` | — | Extra rules for the model |

## Development

```bash
go test ./...
go vet ./...
make build        # or: go build -o commitgen .
```

Releases are built with [GoReleaser](https://goreleaser.com): `goreleaser release --snapshot --clean`.

## License

MIT
