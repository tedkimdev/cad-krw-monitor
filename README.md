# cad-krw-monitor

A lightweight Go service that monitors the CAD/KRW exchange rate, sends Discord alerts, and pushes metrics to Grafana Cloud.

## How it works

```
GitHub Actions (every 20 min)
        ↓ POST /check
Render Web Service (free)
        ↓
  - Fetch CAD/KRW rate
  - Push metrics → Grafana Cloud (Graphite) 📊
  - Send alerts  → Discord 🔔
```

## Alerts

- ⏰ **Hourly** — regular rate update every hour (:00)
- 🚨 **Spike** — immediate alert when rate moves ±0.5% or more (:00, :20, :40)
- 🎯 **Target** — alert when rate hits your target price

## Stack

- **Go** — HTTP server
- **Render** — free web service hosting
- **GitHub Actions** — cron trigger (3x per hour, free on public repos)
- **Grafana Cloud** — metrics dashboard via Graphite (free tier)
- **Discord** — alert notifications

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `DISCORD_WEBHOOK_URL` | ✅ | Discord webhook URL |
| `CHECK_SECRET` | ✅ | Secret header for `/check` endpoint security |
| `GRAPHITE_URL` | ✅ | Grafana Cloud Graphite endpoint URL |
| `GRAPHITE_API_KEY` | ✅ | Grafana Cloud Graphite API key |
| `TARGET_RATE` | ❌ | Target rate in KRW (e.g. `1065`) |
| `TARGET_DIRECTION` | ❌ | `above` or `below` (default: `above`) |

## Deployment

### 1. Render

- New → Blueprint → connect this repo
- Set environment variables in Render dashboard
- Copy the service URL (e.g. `https://cad-krw-monitor.onrender.com`)

### 2. GitHub Secrets

Repo → Settings → Secrets and variables → Actions:

- `RENDER_URL` — your Render service URL
- `CHECK_SECRET` — same value as set in Render

### 3. Grafana Cloud

- grafana.com → My Account → your stack → **Graphite** → Details
- Copy URL and API Key → add to Render env vars

### 4. Test

GitHub Actions → `Check Rate (Hourly :00)` → **Run workflow**

Check Discord for the alert and Grafana for the metric.

## GitHub Actions Workflows

| Workflow | Schedule | Action |
|----------|----------|--------|
| `check-00.yml` | Every hour at :00 | Spike check + hourly update |
| `check-20.yml` | Every hour at :20 | Spike check |
| `check-40.yml` | Every hour at :40 | Spike check |

## Grafana Dashboard

After data starts flowing:

1. Grafana → **Dashboards** → **New** → **New Dashboard**
2. **Add visualization** → Data source: **Graphite**
3. Metric: `cad_krw.rate`
4. Save as `CAD/KRW Monitor`

## Data Source

[open.er-api.com](https://open.er-api.com) — free, no API key required.