<div align="center">

# CommitGen

**AI-written commit messages from your staged diff. Local-first, private, and fast.**

[![Go Version](https://img.shields.io/github/go-mod/go-version/onetwothird/CommitGen?style=flat-square)](go.mod)
[![Latest Tag](https://img.shields.io/github/v/tag/onetwothird/CommitGen?style=flat-square&label=version)](https://github.com/onetwothird/commitgen/tags)
[![Go Report Card](https://goreportcard.com/badge/github.com/onetwothird/CommitGen?style=flat-square)](https://goreportcard.com/report/github.com/onetwothird/commitgen)
[![Go Reference](https://pkg.go.dev/badge/github.com/onetwothird/CommitGen.svg)](https://pkg.go.dev/github.com/onetwothird/CommitGen)
[![License: MIT](https://img.shields.io/github/license/onetwothird/CommitGen?style=flat-square)](LICENSE)

[Features](#features) •
[Installation](#installation) •
[Quick Start](#quick-start) •
[Usage](#usage) •
[Providers](#providers) •
[Configuration](#configuration) •
[FAQ](#faq)

</div>

---

```console
$ git add .
$ CommitGen

  feat(auth): add refresh token rotation

  - rotate refresh tokens on every use to limit replay attacks
  - store token family id so reuse revokes the whole chain

[c]ommit  [e]dit  [r]egenerate  [q]uit (c):
```

## Why CommitGen?

You commit many times a day, so writing good messages shouldn't slow you down or send your code to someone else's server.

- **Private:** it runs against a local model through [Ollama](https://ollama.com) by default. Your code stays on your machine.
- **Fast:** a single static binary with zero dependencies and no runtime to install.
- **Your rules:** Conventional Commits, your scopes, your language and your team's style.

## Features

| | |
|---|---|
| 🔒 **Local-first** | Works out of the box with Ollama, LM Studio, or llama.cpp. No API key required. |
| 🔌 **Any provider** | Any OpenAI-compatible API (OpenAI, Groq, OpenRouter, vLLM), plus Anthropic. |
| 🛡️ **Secret masking** | API keys, tokens, passwords, and private keys are redacted before anything is sent. |
| 🧠 **Smart diffs** | Skips lockfiles, minified files, and build output. Big diffs are trimmed while keeping every file header. |
| 🎯 **Style matching** | Learns from your recent commits and branch name to match your repo's conventions. |
| 🪝 **Git hook** | `CommitGen hook install`, then plain `git commit` opens with a drafted message. |
| 📦 **One commit per file** | `CommitGen -a --each` commits every changed file separately, each with its own message. |
| 🎲 **Multiple candidates** | `-n 3` generates three options to pick from. |
| ⚙️ **Configurable** | Global config plus per-repo `.CommitGen.toml` for scopes, style, language, and custom rules. |

## Installation

### With Go (1.22+)

```bash
go install github.com/onetwothird/CommitGen@latest
```

### From source

```bash
git clone https://github.com/onetwothird/CommitGen.git
cd CommitGen
go build -o CommitGen .         # Windows: go build -o CommitGen.exe .
```

Move the binary to a directory on your `PATH`.

<details>
<summary><b>Adding CommitGen to PATH on Windows</b></summary>

```powershell
$p = [Environment]::GetEnvironmentVariable("Path", "User")
[Environment]::SetEnvironmentVariable("Path", "$p;C:\path\to\CommitGen", "User")
```

Restart your terminal afterwards.
</details>

## Quick Start

New to CommitGen? Follow these steps once and you're set.

### 1. Install the requirements

| Tool | Why | Get it |
|---|---|---|
| **Git** | CommitGen reads your staged changes | [git-scm.com](https://git-scm.com/downloads) |
| **Go 1.22+** | to install CommitGen | [go.dev/dl](https://go.dev/dl/) |
| **Ollama** | runs the AI model locally (skip if you use a cloud API) | [ollama.com/download](https://ollama.com/download) |

Restart your terminal afterwards, then check:

```bash
git --version
go version
ollama --version
```

### 2. Install CommitGen

```bash
go install github.com/onetwothird/CommitGen@latest
CommitGen version
```

> **"CommitGen is not recognized"?** Go installs programs to `~/go/bin` (Windows: `%USERPROFILE%\go\bin`). Add that folder to your `PATH` and restart your terminal.

### 3. Download a model (one time, about 5 GB)

```bash
ollama pull qwen2.5-coder:7b
```

Keep the Ollama app running in the background. On a low-memory machine, use `qwen2.5-coder:3b` and run CommitGen with `--model qwen2.5-coder:3b`.

### 4. Write your first commit message

Inside any git repository:

```bash
git add -A
CommitGen
```

CommitGen shows a drafted message and asks what to do:

| Key | Action |
|---|---|
| `Enter` or `c` | Commit with this message |
| `e` | Open the message in your editor to tweak it, then commit |
| `r` | Generate a new message |
| `q` | Quit without committing |

The first run can take 10–30 seconds while the model loads. Later runs are faster.

### 5. Pick your workflow

```bash
CommitGen -a --each      # commit every changed file separately, automatically
CommitGen -c -y          # generate and commit in one step, no questions
CommitGen hook install   # make plain `git commit` draft the message for you
```

## Usage

```text
CommitGen [flags]
CommitGen hook install | uninstall
CommitGen config init | path | show
CommitGen version
```

| Flag | Description |
|---|---|
| `-c` | Generate, then open `git commit` in your editor to review |
| `-y` | With `-c`: commit immediately without opening the editor |
| `-p` | Print the message only (for scripts and aliases) |
| `-a` | Stage all changes first (like `git add -A`) |
| `--each` | Commit every staged file separately, each with its own message (no prompts) |
| `-n <num>` | Generate several candidates and pick one |
| `-m "<text>"` | Give the model extra context, e.g. `-m "closes #42"` |
| `--dry-run` | Show exactly what would be sent to the model, then exit |
| `--model <name>` | Override the model |
| `--url <url>` | Override the API base URL |
| `--provider <name>` | `openai` or `anthropic` |
| `--style <name>` | `conventional` or `simple` |
| `--lang <language>` | Language of the message |
| `--max <chars>` | Max diff characters sent to the model |
| `--no-redact` | Disable secret masking |
| `-v` | Verbose output |

### One commit per file

```bash
CommitGen -a --each        # stage everything, then commit file by file
CommitGen --each           # only the files you already staged
CommitGen -a --each -p     # preview the messages without committing
```

CommitGen commits exactly what you staged, including partially staged files. Unstaged changes are left alone. If a step fails, it stops and leaves the remaining files staged.

**Tip:** add a git alias:

```bash
git config --global alias.ai '!CommitGen -c'
git ai
```

## Providers

CommitGen talks to any **OpenAI-compatible** endpoint, plus Anthropic's native API.

| Provider | `base_url` | Example model | API key env var |
|---|---|---|---|
| Ollama *(default)* | `http://localhost:11434/v1` | `qwen2.5-coder:7b` | — |
| LM Studio | `http://localhost:1234/v1` | *(loaded model)* | — |
| OpenAI | `https://api.openai.com/v1` | `gpt-4o-mini` | `OPENAI_API_KEY` |
| Groq | `https://api.groq.com/openai/v1` | `llama-3.1-8b-instant` | `CommitGen` |
| OpenRouter | `https://openrouter.ai/api/v1` | `meta-llama/llama-3.1-8b-instruct` | `COMMITGEN_API_KEY` |
| Anthropic | *(set `provider = "anthropic"`)* | `claude-haiku-4-5` | `ANTHROPIC_API_KEY` |

<details>
<summary><b>Example: OpenAI</b></summary>

```bash
# macOS / Linux
export COMMITGEN_BASE_URL=https://api.openai.com/v1
export COMMITGEN_MODEL=gpt-4o-mini
export OPENAI_API_KEY=sk-...
```

```powershell
# Windows PowerShell
$env:COMMITGEN_BASE_URL = "https://api.openai.com/v1"
$env:COMMITGEN_MODEL    = "gpt-4o-mini"
$env:OPENAI_API_KEY     = "sk-..."
```
</details>

<details>
<summary><b>Example: Anthropic</b></summary>

```bash
export COMMITGEN_PROVIDER=anthropic
export COMMITGEN_MODEL=claude-haiku-4-5
export ANTHROPIC_API_KEY=sk-ant-...
```
</details>

## Configuration

Create a starter config with:

```bash
CommitGen config init     # writes the global config
CommitGen config path     # shows where it lives
CommitGen config show     # shows the effective settings
```

Add a `.CommitGen.toml` to a repository root to set project rules:

```toml
style        = "conventional"
scopes       = ["api", "ui", "cli"]
exclude      = ["*.snap", "docs/generated/*"]
instructions = "Reference the Jira ticket from the branch name, e.g. PROJ-123."
```

**Precedence:** defaults → global config → repo `.CommitGen.toml` → environment variables → flags

<details>
<summary><b>All options</b></summary>

| Key | Default | Description |
|---|---|---|
| `provider` | `openai` | `openai` (any OpenAI-compatible API), `ollama`, or `anthropic`. Ollama on port 11434 is detected automatically |
| `base_url` | `http://localhost:11434/v1` | API base URL |
| `model` | `qwen2.5-coder:7b` | Model name |
| `api_key` | — | Prefer env vars: `COMMITGEN_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY` |
| `style` | `conventional` | `conventional` or `simple` |
| `language` | `English` | Language of the message |
| `max_diff` | `12000` | Max diff characters sent |
| `history` | `10` | Recent commit subjects used for style matching |
| `temperature` | `0.2` | Sampling temperature |
| `timeout` | `120` | Request timeout in seconds |
| `num_ctx` | `8192` | Ollama context window in tokens. Raise it for large diffs if you have the RAM |
| `exclude` | — | Extra pathspecs to leave out of the diff |
| `scopes` | — | Allowed Conventional Commit scopes |
| `instructions` | — | Extra rules for the model |

Environment variables: `COMMITGEN_PROVIDER`, `COMMITGEN_BASE_URL`, `COMMITGEN_MODEL`, `COMMITGEN_API_KEY`, `COMMITGEN_STYLE`, `COMMITGEN_LANGUAGE`, `COMMITGEN_CONFIG`.
</details>

## How it works

```text
git diff --cached  →  filter noise  →  mask secrets  →  build prompt  →  LLM  →  clean output  →  git commit
```

1. Reads your **staged** changes only. Nothing unstaged is ever sent.
2. Drops lockfiles, minified assets, and build output, then trims oversized diffs.
3. Redacts anything that looks like a secret.
4. Adds the file summary, branch name, and recent commit subjects for context.
5. Cleans the model's reply (strips code fences, quotes, and preambles). If the reply isn't a valid commit message, it asks the model to rewrite it.
6. Hands the message to `git commit`.

## FAQ

<details>
<summary><b>Does my code leave my machine?</b></summary>

Not with the default setup. CommitGen talks to a local Ollama server. If you configure a cloud provider, the filtered and redacted diff is sent to that provider. Run `CommitGen --dry-run` to see exactly what would be sent.
</details>

<details>
<summary><b>Which local model should I use?</b></summary>

`qwen2.5-coder:7b` is a good default. On machines with less memory, try `qwen2.5-coder:3b` or `llama3.2:3b`.
</details>

<details>
<summary><b>The model describes my diff instead of writing a commit message</b></summary>

This happens with small local models on large diffs. CommitGen detects it and asks the model to rewrite its answer automatically. If it still happens:

- Commit in smaller pieces (`git add` one feature at a time). That also gives you a cleaner history.
- Add context: `CommitGen -m "add accessibility settings"`
- Use a larger model (`--model qwen2.5-coder:14b`) or raise `num_ctx` in your config.
</details>

<details>
<summary><b>"could not reach http://localhost:11434"</b></summary>

Ollama isn't running. Start the Ollama app or run `ollama serve`.
</details>

<details>
<summary><b>Will the git hook block my commits if something fails?</b></summary>

No. If generation fails, the hook exits quietly and git continues as normal. It also skips merges, amends, and commits made with `-m`.
</details>

## Development

```bash
go test ./...
go vet ./...
go build -o CommitGen .
```

Project layout:

```text
.
├── main.go              # CLI entry point and commands
└── internal/
    ├── config/          # config files and env vars
    ├── git/             # git commands (diff, log, branch)
    ├── llm/             # Ollama, OpenAI-compatible and Anthropic clients
    ├── prompt/          # prompt building and output cleanup
    └── redact/          # secret masking
```

## Contributing

Contributions are welcome.

1. Fork the repo and create a branch: `git checkout -b feat/my-feature`
2. Make your changes and add tests
3. Run `go test ./...`
4. Commit (with CommitGen, of course) and open a pull request

## License

Released under the [MIT License](LICENSE).
