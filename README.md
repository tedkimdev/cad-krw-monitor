# cad-krw-monitor

A lightweight Go daemon that monitors the CAD/KRW exchange rate and sends Discord alerts.

## Alerts

- ⏰ **Hourly** — regular rate update every hour
- 🚨 **Spike** — immediate alert when rate moves ±0.5% or more
- 🎯 **Target** — alert when rate hits your target price

## Requirements

- Go 1.21+
- Discord webhook URL
- A place to run it (Render, Fly.io, VPS, or locally)

## Quickstart

```bash
git clone https://github.com/YOUR_USERNAME/cad-krw-monitor
cd cad-krw-monitor
go mod init cad-krw-monitor

# Run with Discord alerts only
DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/..." go run main.go

# Run with a target rate (alert when rate drops to 900 KRW or below)
DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/..." \
TARGET_RATE=900 \
TARGET_DIRECTION=below \
go run main.go
```

## Environment Variables

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `DISCORD_WEBHOOK_URL` | ✅ | Discord webhook URL | `https://discord.com/api/webhooks/...` |
| `TARGET_RATE` | ❌ | Target rate in KRW | `950` |
| `TARGET_DIRECTION` | ❌ | `above` or `below` (default: `above`) | `below` |

## Configuration

Edit the `Config` struct in `main.go`:

```go
config: Config{
    CheckInterval:  5 * time.Minute, // how often to fetch the rate
    NotifyInterval: 1 * time.Hour,   // how often to send a regular update
    SpikeThreshold: 0.005,           // 0.005 = 0.5%
}
```

## Discord Webhook Setup

1. Open the Discord channel you want alerts in
2. Channel Settings → Integrations → Webhooks → New Webhook
3. Copy the webhook URL

## Deployment

### Render (recommended — free tier)

1. Push this repo to GitHub
2. Create a new **Background Worker** on Render
3. Set environment variables in Render dashboard
4. Deploy

### Run in background (macOS/Linux)

```bash
go build -o monitor main.go

nohup DISCORD_WEBHOOK_URL="..." ./monitor > monitor.log 2>&1 &

# View logs
tail -f monitor.log

# Stop
kill $(pgrep monitor)
```

### systemd (Linux)

```ini
# /etc/systemd/system/cad-krw-monitor.service
[Unit]
Description=CAD/KRW Exchange Rate Monitor

[Service]
Environment=DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/...
ExecStart=/path/to/monitor
Restart=always

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now cad-krw-monitor
```

## Data Source

Uses [open.er-api.com](https://open.er-api.com) — free, no API key required.
