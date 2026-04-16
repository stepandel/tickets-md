package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type serverInfo struct {
	Port int `json:"port"`
	Pid  int `json:"pid"`
}

type terminalServerError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *terminalServerError) Error() string {
	if e.Body == "" {
		return e.Status
	}
	return fmt.Sprintf("%s: %s", e.Status, e.Body)
}

func readTerminalServer(root string) (*serverInfo, error) {
	data, err := os.ReadFile(terminalServerFilePath(root))
	if err != nil {
		return nil, err
	}
	var si serverInfo
	if err := json.Unmarshal(data, &si); err != nil {
		return nil, err
	}
	return &si, nil
}

func postTerminalServer(root, path string, body any) (string, error) {
	si, err := readTerminalServer(root)
	if err != nil {
		return "", fmt.Errorf("terminal server not running — start `tickets watch`")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", si.Port, path)
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		return "", &terminalServerError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       string(buf[:n]),
		}
	}
	var out struct {
		Session string `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Session, nil
}
