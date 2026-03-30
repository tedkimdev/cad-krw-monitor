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
	"sync"
	"time"
)

type Config struct {
	DiscordWebhookURL string
	SpikeThreshold    float64
	TargetRate        float64
	TargetAbove       bool
	CheckSecret       string
	GraphiteURL       string
	GraphiteUser      string
	GraphiteAPIKey    string
}

type State struct {
	mu        sync.Mutex
	prevRate  float64
	targetHit bool
}

var (
	cfg   Config
	state = &State{}
)

func fetchRate() (float64, error) {
	resp, err := http.Get("https://open.er-api.com/v6/latest/CAD")
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
	}
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

// pushGraphite sends rate to Grafana Cloud via Graphite plaintext protocol
func pushGraphite(rate float64) error {
	if cfg.GraphiteURL == "" {
		return nil
	}

	ts := time.Now().Unix()
	// Graphite plaintext: "metric.name value timestamp\n"
	line := fmt.Sprintf("cad_krw.rate %.4f %d\n", rate, ts)

	req, err := http.NewRequest(http.MethodPost, cfg.GraphiteURL, bytes.NewBufferString(line))
	if err != nil {
		return err
	}
	req.SetBasicAuth(cfg.GraphiteUser, cfg.GraphiteAPIKey)
	req.Header.Set("Content-Type", "text/plain")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("graphite error %d: %s", resp.StatusCode, string(b))
	}
	log.Printf("graphite: pushed %.4f KRW", rate)
	return nil
}

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

func sendDiscord(embed DiscordEmbed) error {
	payload := DiscordPayload{Username: "CAD/KRW Monitor", Embeds: []DiscordEmbed{embed}}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(cfg.DiscordWebhookURL, "application/json", bytes.NewBuffer(data))
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

func buildEmbed(rate, changePct float64, alertType string) DiscordEmbed {
	var color int
	var title string
	switch alertType {
	case "hourly":
		color = 0x5865F2
		title = "⏰ Hourly Rate Update"
	case "spike":
		if changePct > 0 {
			color = 0x57F287
			title = fmt.Sprintf("🚨 Spike — Rate jumped +%.2f%%", changePct)
		} else {
			color = 0xED4245
			title = fmt.Sprintf("🚨 Spike — Rate dropped %.2f%%", changePct)
		}
	case "target":
		color = 0xFEE75C
		title = "🎯 Target Rate Reached!"
	}

	changeStr := fmt.Sprintf("%+.2f%%", changePct)
	if changePct == 0 {
		changeStr = "first reading"
	}

	embed := DiscordEmbed{
		Title:     title,
		Color:     color,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Fields: []DiscordField{
			{Name: "💵 Current Rate", Value: fmt.Sprintf("**1 CAD = %.2f KRW**", rate), Inline: true},
			{Name: trendEmoji(changePct) + " Change", Value: changeStr, Inline: true},
		},
		Footer: &DiscordFooter{Text: "Source: open.er-api.com"},
	}

	if cfg.TargetRate > 0 {
		dir := "or above"
		if !cfg.TargetAbove {
			dir = "or below"
		}
		embed.Fields = append(embed.Fields, DiscordField{
			Name:   "🎯 Target",
			Value:  fmt.Sprintf("%.2f KRW %s", cfg.TargetRate, dir),
			Inline: true,
		})
	}
	return embed
}

func check(hourly bool) (float64, error) {
	rate, err := fetchRate()
	if err != nil {
		return 0, err
	}
	log.Printf("rate: 1 CAD = %.2f KRW", rate)

	// Push to Graphite
	if err := pushGraphite(rate); err != nil {
		log.Printf("WARN graphite push failed: %v", err)
	}

	state.mu.Lock()
	prevRate := state.prevRate
	targetHit := state.targetHit
	state.mu.Unlock()

	var changePct float64
	if prevRate > 0 {
		changePct = (rate - prevRate) / prevRate * 100
	}

	// Spike detection
	if prevRate > 0 && math.Abs(changePct) >= cfg.SpikeThreshold*100 {
		log.Printf("spike detected: %+.2f%%", changePct)
		if err := sendDiscord(buildEmbed(rate, changePct, "spike")); err != nil {
			log.Printf("ERROR sending spike alert: %v", err)
		} else {
			log.Println("spike alert sent")
		}
	}

	// Target check
	if cfg.TargetRate > 0 && !targetHit {
		hit := (cfg.TargetAbove && rate >= cfg.TargetRate) ||
			(!cfg.TargetAbove && rate <= cfg.TargetRate)
		if hit {
			log.Printf("target reached: %.2f KRW", rate)
			if err := sendDiscord(buildEmbed(rate, changePct, "target")); err != nil {
				log.Printf("ERROR sending target alert: %v", err)
			} else {
				state.mu.Lock()
				state.targetHit = true
				state.mu.Unlock()
				log.Println("target alert sent")
			}
		}
	}

	// Hourly update
	if hourly {
		if err := sendDiscord(buildEmbed(rate, changePct, "hourly")); err != nil {
			log.Printf("ERROR sending hourly update: %v", err)
		} else {
			log.Println("hourly update sent")
		}
	}

	state.mu.Lock()
	state.prevRate = rate
	state.mu.Unlock()

	return rate, nil
}

func handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cfg.CheckSecret != "" && r.Header.Get("X-Secret") != cfg.CheckSecret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	hourly := r.URL.Query().Get("hourly") == "true"
	rate, err := check(hourly)
	if err != nil {
		log.Printf("ERROR: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"rate": rate, "time": time.Now().UTC().Format(time.RFC3339)})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	state.mu.Lock()
	prev := state.prevRate
	state.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "prev_rate": prev})
}

func main() {
	cfg = Config{
		DiscordWebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
		SpikeThreshold:    0.005,
		CheckSecret:       os.Getenv("CHECK_SECRET"),
		GraphiteURL:       os.Getenv("GRAPHITE_URL"),
		GraphiteUser:      os.Getenv("GRAPHITE_USER"),
		GraphiteAPIKey:    os.Getenv("GRAPHITE_API_KEY"),
	}
	if cfg.DiscordWebhookURL == "" {
		log.Fatal("DISCORD_WEBHOOK_URL env var is required")
	}
	if t := os.Getenv("TARGET_RATE"); t != "" {
		if val, err := strconv.ParseFloat(t, 64); err == nil {
			cfg.TargetRate = val
		}
	}
	cfg.TargetAbove = os.Getenv("TARGET_DIRECTION") != "below"

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/check", handleCheck)
	http.HandleFunc("/health", handleHealth)

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
