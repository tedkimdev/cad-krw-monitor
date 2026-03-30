package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"
)

// --- Config ---
type Config struct {
	DiscordWebhookURL string
	CheckInterval     time.Duration // how often to fetch the rate
	NotifyInterval    time.Duration // how often to send a regular update (1 hour)
	SpikeThreshold    float64       // spike alert threshold (0.005 = 0.5%)
	TargetRate        float64       // target rate alert (0 = disabled)
	TargetAbove       bool          // true: alert when rate >= target, false: when rate <= target
}

// --- Exchange Rate API response ---
type RateResponse struct {
	Result string             `json:"result"`
	Rates  map[string]float64 `json:"rates"`
}

// --- Discord webhook payload ---
type DiscordEmbed struct {
	Title     string         `json:"title"`
	Color     int            `json:"color"`
	Fields    []DiscordField `json:"fields,omitempty"`
	Footer    *DiscordFooter `json:"footer,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
}

type DiscordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type DiscordFooter struct {
	Text string `json:"text"`
}

type DiscordPayload struct {
	Username string         `json:"username"`
	Embeds   []DiscordEmbed `json:"embeds"`
}

// --- Monitor state ---
type Monitor struct {
	config     Config
	prevRate   float64
	lastNotify time.Time
	targetHit  bool
}

func fetchRate() (float64, error) {
	resp, err := http.Get("https://open.er-api.com/v6/latest/CAD")
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result RateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("json parse error: %w", err)
	}
	if result.Result != "success" {
		return 0, fmt.Errorf("api error: %s", result.Result)
	}
	rate, ok := result.Rates["KRW"]
	if !ok {
		return 0, fmt.Errorf("KRW rate not found")
	}
	return rate, nil
}

func (m *Monitor) sendDiscord(embed DiscordEmbed) error {
	payload := DiscordPayload{
		Username: "CAD/KRW Monitor",
		Embeds:   []DiscordEmbed{embed},
	}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(m.config.DiscordWebhookURL, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func trendEmoji(change float64) string {
	if change > 0 {
		return "📈"
	} else if change < 0 {
		return "📉"
	}
	return "➡️"
}

func (m *Monitor) buildEmbed(rate, change float64, alertType string) DiscordEmbed {
	var color int
	var title string

	switch alertType {
	case "hourly":
		color = 0x5865F2
		title = "⏰ Hourly Rate Update"
	case "spike":
		if change > 0 {
			color = 0x57F287
			title = fmt.Sprintf("🚨 Spike Alert — Rate jumped +%.2f%%", change)
		} else {
			color = 0xED4245
			title = fmt.Sprintf("🚨 Spike Alert — Rate dropped %.2f%%", change)
		}
	case "target":
		color = 0xFEE75C
		title = "🎯 Target Rate Reached!"
	}

	changeStr := fmt.Sprintf("%+.2f%%", change)
	if m.prevRate == 0 {
		changeStr = "first reading"
	}

	embed := DiscordEmbed{
		Title:     title,
		Color:     color,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Fields: []DiscordField{
			{Name: "💵 Current Rate", Value: fmt.Sprintf("**1 CAD = %.2f KRW**", rate), Inline: true},
			{Name: trendEmoji(change) + " Change", Value: changeStr, Inline: true},
		},
		Footer: &DiscordFooter{Text: "Source: open.er-api.com"},
	}

	if m.config.TargetRate > 0 {
		dir := "or above"
		if !m.config.TargetAbove {
			dir = "or below"
		}
		embed.Fields = append(embed.Fields, DiscordField{
			Name:   "🎯 Target",
			Value:  fmt.Sprintf("%.2f KRW %s", m.config.TargetRate, dir),
			Inline: true,
		})
	}

	return embed
}

func (m *Monitor) run() {
	log.Println("CAD/KRW monitor started")
	log.Printf("  check interval : %v", m.config.CheckInterval)
	log.Printf("  notify interval: %v", m.config.NotifyInterval)
	log.Printf("  spike threshold: ±%.1f%%", m.config.SpikeThreshold*100)
	if m.config.TargetRate > 0 {
		dir := "above"
		if !m.config.TargetAbove {
			dir = "below"
		}
		log.Printf("  target rate    : %.2f KRW (%s)", m.config.TargetRate, dir)
	}

	for {
		rate, err := fetchRate()
		if err != nil {
			log.Printf("ERROR fetching rate: %v", err)
			time.Sleep(m.config.CheckInterval)
			continue
		}

		now := time.Now()
		log.Printf("rate: 1 CAD = %.2f KRW", rate)

		var changePct float64
		if m.prevRate > 0 {
			changePct = (rate - m.prevRate) / m.prevRate * 100
		}

		// 1. Spike alert
		if m.prevRate > 0 && math.Abs(changePct) >= m.config.SpikeThreshold*100 {
			log.Printf("spike detected: %+.2f%%", changePct)
			if err := m.sendDiscord(m.buildEmbed(rate, changePct, "spike")); err != nil {
				log.Printf("ERROR sending spike alert: %v", err)
			} else {
				log.Println("spike alert sent")
			}
		}

		// 2. Target rate alert
		if m.config.TargetRate > 0 && !m.targetHit {
			hit := (m.config.TargetAbove && rate >= m.config.TargetRate) ||
				(!m.config.TargetAbove && rate <= m.config.TargetRate)
			if hit {
				log.Printf("target rate reached: %.2f KRW", rate)
				if err := m.sendDiscord(m.buildEmbed(rate, changePct, "target")); err != nil {
					log.Printf("ERROR sending target alert: %v", err)
				} else {
					log.Println("target alert sent")
					m.targetHit = true
				}
			}
		}

		// 3. Hourly update
		if now.Sub(m.lastNotify) >= m.config.NotifyInterval {
			if err := m.sendDiscord(m.buildEmbed(rate, changePct, "hourly")); err != nil {
				log.Printf("ERROR sending hourly update: %v", err)
			} else {
				log.Println("hourly update sent")
				m.lastNotify = now
			}
		}

		m.prevRate = rate
		time.Sleep(m.config.CheckInterval)
	}
}

func main() {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		log.Fatal("DISCORD_WEBHOOK_URL env var is required")
	}

	targetRate := 0.0
	targetAbove := true

	if t := os.Getenv("TARGET_RATE"); t != "" {
		if val, err := strconv.ParseFloat(t, 64); err == nil {
			targetRate = val
		}
	}
	if os.Getenv("TARGET_DIRECTION") == "below" {
		targetAbove = false
	}

	m := &Monitor{
		config: Config{
			DiscordWebhookURL: webhookURL,
			CheckInterval:     5 * time.Minute,
			NotifyInterval:    1 * time.Hour,
			SpikeThreshold:    0.005,
			TargetRate:        targetRate,
			TargetAbove:       targetAbove,
		},
	}

	// trigger first hourly update immediately on start
	m.lastNotify = time.Now().Add(-m.config.NotifyInterval)

	m.run()
}
