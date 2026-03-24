package protocol

type CheckinRequest struct {
	DeviceID     int64              `json:"deviceId"`
	Token        string             `json:"token"`
	Hostname     string             `json:"hostname"`
	OS           string             `json:"os"`
	Arch         string             `json:"arch"`
	AgentVersion string             `json:"agentVersion"`
	IPs          []string           `json:"ips"`
	Metrics      map[string]float64 `json:"metrics"`
	Inventory    map[string]string  `json:"inventory,omitempty"`
}

type RegisterRequest struct {
	EnrollmentToken string            `json:"enrollmentToken"`
	Hostname        string            `json:"hostname"`
	OS              string            `json:"os"`
	Arch            string            `json:"arch"`
	AgentVersion    string            `json:"agentVersion"`
	IPs             []string          `json:"ips"`
	Inventory       map[string]string `json:"inventory,omitempty"`
}

type RegisterResponse struct {
	DeviceID int64  `json:"deviceId"`
	Token    string `json:"token"`
}

type CheckinResponse struct {
	Jobs []Job `json:"jobs"`
}

type Job struct {
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	PayloadRaw string `json:"payload_json"`
}

type JobResultRequest struct {
	DeviceID int64  `json:"deviceId"`
	Token    string `json:"token"`
	JobID    int64  `json:"jobId"`
	Status   string `json:"status"`
	Output   string `json:"output"`
}
