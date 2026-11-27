# xapp (Cloud Run Job for Ireland News X Bot)

A Go-based Google Cloud Run Job that fetches the latest Irish news via Gemini API (Google Search grounding), summarizes and translates it into Japanese, resolves source article URLs from grounding metadata, and publishes each item as a standalone post on X (formerly Twitter).

Built with the Go standard library only (no external dependencies) and packaged into a minimal distroless container.

## Directory Structure

```text
.
├── Dockerfile
├── .dockerignore
├── .env.example
├── .gitignore
├── cloudbuild.yaml
├── go.mod
├── prompts/
│   └── news_prompt.txt          # Editable Gemini prompt template ({{TODAY}} placeholder)
├── cmd/
│   └── job/
│       └── main.go              # Cloud Run Job entrypoint (JSON structured logging)
└── internal/
    ├── config/
    │   └── config.go            # Environment variable loading and validation
    ├── news/
    │   ├── fetcher.go           # Gemini API client with Google Search grounding
    │   └── fetcher_test.go      # Unit tests
    └── xclient/
        ├── client.go            # X API v2 OAuth 1.0a client (single tweet & thread)
        └── client_test.go       # Unit tests
```

## Configuration

| Environment Variable | Required | Default | Description |
| :--- | :--- | :--- | :--- |
| `GEMINI_API_KEY` | Yes | - | Gemini API key |
| `GEMINI_MODEL` | No | `gemini-4-hydrogen` | Gemini model ID |
| `PROMPT_FILE` | No | `prompts/news_prompt.txt` | Path to prompt template file |
| `X_API_KEY` | When `DRY_RUN=false` | - | X API Key (OAuth 1.0a Consumer Key) |
| `X_API_KEY_SECRET` | When `DRY_RUN=false` | - | X API Key Secret (OAuth 1.0a Consumer Secret) |
| `X_ACCESS_TOKEN` | When `DRY_RUN=false` | - | X Access Token (Read and write) |
| `X_ACCESS_TOKEN_SECRET` | When `DRY_RUN=false` | - | X Access Token Secret |
| `DRY_RUN` | No | `false` | Set to `true` to fetch news without posting to X |

## Local Development

```bash
# Run unit tests
go test -v ./...

# Copy and fill in environment variables
cp .env.example .env
set -a && source .env && set +a

# Dry run (fetches news via Gemini without posting to X)
DRY_RUN=true go run ./cmd/job

# Live run (posts thread to X)
DRY_RUN=false go run ./cmd/job
```

## Prompt Customization

Edit `prompts/news_prompt.txt` to customize the tone, number of news items, or hashtags.
- `{{TODAY}}` is replaced at runtime with the current UTC date (`YYYY-MM-DD`).
- Separate multiple posts with a line containing only `---`; each section is posted independently on X with its resolved grounding source URL appended.

## Deployment (Cloud Run Job & Cloud Build)

Pushing to `main` triggers `cloudbuild.yaml`, which builds the container image, pushes it to Artifact Registry, and updates the `x-ireland-news` Cloud Run Job.

To run the deployed job manually:

```bash
gcloud run jobs execute x-ireland-news \
  --region=asia-northeast1 \
  --project=<PROJECT_ID> \
  --wait \
  --quiet
```
