// Package transport 定义 HTTP 传输层使用的响应结构。

package transport

// LogEntry 表示一条带绝对时间戳的结构化日志记录。
type LogEntry struct {
	Timestamp string `json:"timestamp,omitempty"`
	Message   string `json:"message,omitempty"`
}

// InfoResponse 表示信息类接口共用的 JSON 响应。
type InfoResponse struct {
	OK         bool       `json:"ok"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	Logs       string     `json:"logs,omitempty"`
	LogEntries []LogEntry `json:"log_entries,omitempty"`
}

// ScreenshotJobResponse 表示截图后台任务的创建结果、状态查询结果和最终产出。
type ScreenshotJobResponse struct {
	OK          bool       `json:"ok"`
	JobID       string     `json:"job_id,omitempty"`
	Status      string     `json:"status,omitempty"`
	Mode        string     `json:"mode,omitempty"`
	Output      string     `json:"output,omitempty"`
	DownloadURL string     `json:"download_url,omitempty"`
	Error       string     `json:"error,omitempty"`
	Logs        string     `json:"logs,omitempty"`
	LogEntries  []LogEntry `json:"log_entries,omitempty"`
}

// InfoJobResponse 表示信息类后台任务的创建结果、状态查询结果和最终输出。
type InfoJobResponse struct {
	OK         bool       `json:"ok"`
	JobID      string     `json:"job_id,omitempty"`
	Status     string     `json:"status,omitempty"`
	Kind       string     `json:"kind,omitempty"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	Logs       string     `json:"logs,omitempty"`
	LogEntries []LogEntry `json:"log_entries,omitempty"`
}

// PathItem 表示路径联想接口返回的一条候选路径。
type PathItem struct {
	Path     string `json:"path"`
	IsDir    bool   `json:"isDir,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Duration string `json:"duration,omitempty"`
}

// PathResponse 表示路径联想接口的 JSON 响应。
type PathResponse struct {
	OK    bool       `json:"ok"`
	Root  string     `json:"root,omitempty"`
	Roots []string   `json:"roots,omitempty"`
	Items []PathItem `json:"items,omitempty"`
	Error string     `json:"error,omitempty"`
}
