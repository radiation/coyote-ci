package api

type ServerInfoResponse struct {
	Version      string   `json:"version"`
	Commit       string   `json:"commit,omitempty"`
	BuildDate    string   `json:"build_date,omitempty"`
	APIVersion   string   `json:"api_version"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type ServerInfoEnvelope struct {
	Data ServerInfoResponse `json:"data"`
}
