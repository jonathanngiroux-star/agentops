# Agent Ops Retainer

Multi-agent orchestration hosting, failure recovery, version update management — "someone is watching" your agent fleet.

## Service Description

AgentOps provides managed hosting for multi-agent systems. Instead of running agents ad-hoc on developer machines, AgentOps runs them as supervised processes with:

- **Automatic restart** on failure with exponential backoff
- **Health checks** (HTTP/TCP/custom) with alerting
- **Version management** — rolling updates with rollback
- **Centralized logging** and metrics collection
- **SLA-backed uptime** with proportional credits

Pricing: **$99–299/mo** (Accountability tier)

## Installation

```bash
# Build from source
go build -o agentops .

# Or install directly
go install github.com/jonathanngiroux-star/agentops@latest
```

## Configuration

Create a config file at `/etc/agentops/agents.json` (or specify with `-config`):

```json
[
  {
    "id": "uuid-here",
    "name": "trading-bot",
    "command": "/usr/local/bin/hermes",
    "args": ["run", "--profile", "trading"],
    "env": {
      "HERMES_CONFIG": "/etc/hermes/trading.yaml"
    },
    "working_dir": "/opt/agents/trading",
    "auto_restart": true,
    "max_retries": 5,
    "health_check": "http://localhost:8646/health",
    "status": "stopped"
  }
]
```

## CLI Interface

```bash
# Add a new agent
agentops -action add -name "my-agent" -command "/path/to/binary" -args '["--flag"]' -env '{"KEY":"value"}' -workdir "/opt/agent" -health "http://localhost:8080/health"

# List all agents
agentops -action list

# Start/stop/restart an agent
agentops -action start -id <agent-id>
agentops -action stop -id <agent-id>
agentops -action restart -id <agent-id>

# Check agent status
agentops -action status -id <agent-id>

# Health check
agentops -action health -id <agent-id>

# Remove an agent
agentops -action remove -id <agent-id>

# Run as daemon (starts all auto-restart agents)
agentops -action run
```

## API Interface (when running as daemon)

```
GET  /api/agents           - List all agents
GET  /api/agents/:id       - Get agent details
POST /api/agents           - Create new agent
DELETE /api/agents/:id     - Remove agent
POST /api/agents/:id/start - Start agent
POST /api/agents/:id/stop  - Stop agent
POST /api/agents/:id/restart - Restart agent
GET  /api/agents/:id/health - Health check
GET  /api/metrics          - Prometheus metrics
```

## Pricing

| Tier | Price | Includes |
|------|-------|----------|
| Starter | $99/mo | Up to 5 agents, basic health checks, email alerts |
| Professional | $199/mo | Up to 20 agents, Slack/PagerDuty alerts, rolling updates |
| Enterprise | $299/mo | Unlimited agents, custom SLA, on-call rotation, audit logs |

## Hosted Version

Managed hosting available at **katsironni.com/agentops** — includes SLA, managed infrastructure, and 24/7 on-call.

## License

MIT — see [LICENSE](LICENSE)