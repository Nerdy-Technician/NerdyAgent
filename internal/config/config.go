package config

import (
	"bytes"
	"encoding/json"
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
		AgentVersion:   "0.3.5",
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

func Save(path string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}
