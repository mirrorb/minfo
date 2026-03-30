package transport

type InfoResponse struct {
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	Logs   string `json:"logs,omitempty"`
}

type PathResponse struct {
	OK    bool     `json:"ok"`
	Root  string   `json:"root,omitempty"`
	Roots []string `json:"roots,omitempty"`
	Items []string `json:"items,omitempty"`
	Error string   `json:"error,omitempty"`
}
