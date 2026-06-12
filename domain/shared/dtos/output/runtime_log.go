package output

type RuntimeLogDto struct {
	Timestamp int64  `json:"timestamp"`
	Time      string `json:"time"`
	Level     string `json:"level"`
	Source    string `json:"source"`
	Message   string `json:"message"`
	OS        string `json:"os"`
}
