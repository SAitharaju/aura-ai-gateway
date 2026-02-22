# Aura AI Gateway ⚡️

> A blazing-fast, sub-10ms observability and budget-tracking proxy for OpenAI-compatible LLM endpoints. Written in Go.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/SAitharaju/aura-ai-gateway)](https://goreportcard.com/report/github.com/SAitharaju/aura-ai-gateway)

Aura is a lightweight, stateless AI Gateway designed for maximum performance. It sits between your application and your LLM provider (OpenAI, Groq, local models, etc.), intercepts streaming chat completions, and tracks token usage in real-time to enforce strict spending limits—all without adding noticeable latency or buffering the stream.

## Why Aura?

Most LLM proxies are built in Python or Node.js. While feature-rich, they often introduce significant latency overhead, especially when parsing large streaming responses. 

Aura is written purely in Go. By utilizing Go's native concurrent channels and low-level byte scanners, Aura achieves **sub-10ms Time To First Token (TTFT) overhead**, decoupling the actual HTTP stream from the asynchronous billing calculations.

## Core Features

- ⚡️ **Blazing Fast Streaming:** Streams Server-Sent Events (SSE) immediately to the client without buffering.
- 💰 **Real-time Budget Enforcement:** Automatically injects `stream_options`, intercepts the usage chunk mid-stream, and instantly deducts costs from a Valkey/Redis backed Circuit Breaker.
- 📊 **Observability Built-in:** Exposes a `/metrics` endpoint for Prometheus to track request latency, token consumption per API key, and error rates natively.
- 🔌 **Provider Agnostic:** If it speaks the OpenAI `/v1/chat/completions` protocol (e.g., Groq, vLLM, Ollama, Anthropic via adapters), Aura can proxy it.
- 🐳 **Docker Ready:** Comes with a complete `docker-compose.yml` including Valkey, Prometheus, and Grafana.

---

## Quickstart

### Option 1: Docker (Recommended)
You can spin up the entire Aura stack (Proxy, Valkey, Prometheus, Grafana) in seconds:

```bash
docker-compose up --build -d
```

### Option 2: Local Run (In-Memory Testing)
Want to test it out without a database? You can run Aura entirely in-memory:

```bash
# Point upstream to Groq (or default to OpenAI)
export UPSTREAM_URL=https://api.groq.com/openai/v1/chat/completions
export USE_MEMORY_STORE=true

go run cmd/gateway/main.go
```

## Usage

Aura listens on port `:8080`. Simply point your existing OpenAI SDKs or curl commands to `http://localhost:8080` instead of `https://api.openai.com`.

### 1. Execute a Streaming Request
```bash
curl -i -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer YOUR_ACTUAL_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama-3.1-8b-instant", 
    "messages": [{"role": "user", "content": "Explain quantum computing in one sentence."}]
  }'
```
*Notice how fast the stream begins! Aura captures the tokens at the very end and updates the database asynchronously.*

### 2. Check Remaining Budget
Users can query their remaining budget interactively:
```bash
curl -i http://localhost:8080/v1/usage \
  -H "Authorization: Bearer YOUR_ACTUAL_API_KEY"
```
**Response:**
```json
{
  "api_key": "YOUR_ACTUAL_API_KEY",
  "limit_dollars": 10.00,
  "usage_dollars": 0.00019,
  "remaining_dollars": 9.99981
}
```

## Architecture

```text
Client Request --> [ Aura AI Gateway ] --(Streaming Passthrough)--> Upstream LLM (OpenAI/Groq)
                         |
                 (Async Channel)
                         |
                 [ Circuit Breaker ] <---> Valkey / Redis (Usage State)
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
