package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	ServerURL       string        `json:"serverUrl"`
	DeviceID        int64         `json:"deviceId"`
	Token           string        `json:"token"`
	EnrollmentToken string        `json:"enrollmentToken"`
	CheckinEvery    time.Duration `json:"checkinEvery"`
	AgentVersion    string        `json:"agentVersion"`
	JobTimeoutSec   int           `json:"jobTimeoutSec"`
	OutputMaxBytes  int           `json:"outputMaxBytes"`
}

func Load(path string) (Config, error) {
	cfg := Config{
		ServerURL:      "http://localhost:8080",
		CheckinEvery:   30 * time.Second,
		AgentVersion:   "0.3.9.5",
		JobTimeoutSec:  120,
		OutputMaxBytes: 131072,
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.CheckinEvery == 0 {
		cfg.CheckinEvery = 30 * time.Second
	}
	if cfg.JobTimeoutSec == 0 {
		cfg.JobTimeoutSec = 120
	}
	if cfg.OutputMaxBytes == 0 {
		cfg.OutputMaxBytes = 131072
	}
	return cfg, nil
}

func (cfg *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	aux := struct {
		CheckinEvery json.RawMessage `json:"checkinEvery"`
		*Alias
	}{
		Alias: (*Alias)(cfg),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(bytes.TrimSpace(aux.CheckinEvery)) == 0 || bytes.Equal(bytes.TrimSpace(aux.CheckinEvery), []byte("null")) {
		return nil
	}
	var text string
	if err := json.Unmarshal(aux.CheckinEvery, &text); err == nil {
		d, err := time.ParseDuration(text)
		if err != nil {
			return fmt.Errorf("invalid checkinEvery duration %q: %w", text, err)
		}
		cfg.CheckinEvery = d
		return nil
	}
	var nanos int64
	if err := json.Unmarshal(aux.CheckinEvery, &nanos); err == nil {
		cfg.CheckinEvery = time.Duration(nanos)
		return nil
	}
	return fmt.Errorf("invalid checkinEvery duration")
}

func Save(path string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}
