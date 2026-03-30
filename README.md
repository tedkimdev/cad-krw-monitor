# cad-krw-monitor

A lightweight Go service that monitors the CAD/KRW exchange rate, sends Discord alerts, and pushes metrics to Grafana Cloud.

## How it works

```
GitHub Actions (every 20 min)
        ↓ POST /check
Render Web Service (free)
        ↓
  - Fetch CAD/KRW rate (Yahoo Finance → open.er-api.com fallback)
  - Push metrics → Grafana Cloud (Graphite) 📊
  - Send alerts  → Discord 🔔
  - Background goroutine checks every 5 min while awake
```

## Alerts

- ⏰ **Hourly** — regular rate update every hour (:00)
- 🚨 **Spike** — immediate alert when rate moves ±0.5% or more (:00, :20, :40)
- 🎯 **Target** — alert when rate hits your target price

## Data Sources

1. **Yahoo Finance** (primary) — real-time CAD/KRW rate
2. **open.er-api.com** (fallback) — hourly updates, no API key required

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
| `GRAPHITE_URL` | ✅ | Grafana Cloud Graphite ingest URL |
| `GRAPHITE_USER` | ✅ | Grafana Cloud Graphite user ID (numeric) |
| `GRAPHITE_API_KEY` | ✅ | Grafana Cloud access policy token |
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
- Copy ingest URL, user ID, and generate an access policy token with `metrics:write` scope
- Add to Render env vars

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

## Background Checker

When the server is awake (after a GitHub Actions call), a background goroutine checks the rate every 5 minutes for spike detection. The server sleeps after ~15 minutes of inactivity on Render's free tier.

## Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/check?hourly=true` | POST | Fetch rate, push to Grafana, send hourly Discord update |
| `/check` | POST | Fetch rate, push to Grafana, spike/target check only |
| `/health` | GET | Health check |