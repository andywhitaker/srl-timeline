package srlclient

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"timeline/pkg/normalizer"
)

// SRLClient manages realtime interactions with Nokia SR Linux services.
type SRLClient struct {
	JSONRPCURL string
	Username   string
	Password   string
	UseNetNS   bool
	IsOnBox    bool
	HTTPClient *http.Client
}

// NewSRLClient creates a new SRLClient.
func NewSRLClient() *SRLClient {
	url := os.Getenv("SRL_JSONRPC_URL")
	user := os.Getenv("SRL_USER")
	pass := os.Getenv("SRL_PASS")

	// Detect if running on-box with sr_cli available
	isOnBox := false
	if _, err := exec.LookPath("sr_cli"); err == nil {
		isOnBox = true
	} else if _, err := os.Stat("/opt/srlinux/bin/sr_cli"); err == nil {
		isOnBox = true
	} else if _, err := os.Stat("/etc/opt/srlinux"); err == nil {
		isOnBox = true
	}

	useNetNS := false
	if _, err := os.Stat("/var/run/netns/srbase-mgmt"); err == nil {
		useNetNS = true
	}

	if url == "" {
		url = "http://127.0.0.1:80/jsonrpc"
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &SRLClient{
		JSONRPCURL: url,
		Username:   user,
		Password:   pass,
		UseNetNS:   useNetNS,
		IsOnBox:    isOnBox,
		HTTPClient: &http.Client{Transport: tr, Timeout: 10 * time.Second},
	}
}

func (c *SRLClient) postJSONRPC(payload map[string]interface{}) (map[string]interface{}, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	if c.UseNetNS {
		// Execute curl inside srbase-mgmt namespace for flawless reliability
		var cmdArgs []string
		if os.Geteuid() != 0 {
			cmdArgs = []string{"sudo", "-n", "ip", "netns", "exec", "srbase-mgmt"}
		} else {
			cmdArgs = []string{"ip", "netns", "exec", "srbase-mgmt"}
		}
		cmdArgs = append(cmdArgs,
			"curl", "-s",
		)
		if c.Username != "" {
			cmdArgs = append(cmdArgs, "-u", fmt.Sprintf("%s:%s", c.Username, c.Password))
		}
		cmdArgs = append(cmdArgs,
			"-H", "Content-Type: application/json",
			"-d", string(bodyBytes),
			c.JSONRPCURL,
		)
		curlCmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		var stdout, stderr bytes.Buffer
		curlCmd.Stdout = &stdout
		curlCmd.Stderr = &stderr
		if err := curlCmd.Run(); err == nil && stdout.Len() > 0 {
			var resp map[string]interface{}
			if json.Unmarshal(stdout.Bytes(), &resp) == nil {
				return resp, nil
			}
		}
	}

	// Fallback to direct HTTP
	req, err := http.NewRequest("POST", c.JSONRPCURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	if c.Username != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetRunningConfig retrieves the running configuration (or a subtree).
func (c *SRLClient) GetRunningConfig(path string) (map[string]interface{}, error) {
	if path == "" {
		path = "/"
	}

	// When on-box, prioritize zero-credential sr_cli IPC
	if c.IsOnBox {
		if cfg, err := c.getRunningConfigOnBox(path); err == nil && len(cfg) > 0 {
			return cfg, nil
		}
	}

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "get",
		"params": map[string]interface{}{
			"commands": []map[string]interface{}{
				{
					"path":      path,
					"datastore": "running",
				},
			},
		},
	}

	resp, err := c.postJSONRPC(payload)
	if err == nil {
		if resList, ok := resp["result"].([]interface{}); ok && len(resList) > 0 {
			if resMap, ok := resList[0].(map[string]interface{}); ok && len(resMap) > 0 {
				return resMap, nil
			}
		}
	}

	// Fallback to startup config file on disk if path == "/" and services are not yet ready during boot
	if path == "/" {
		if data, readErr := os.ReadFile("/etc/opt/srlinux/config.json"); readErr == nil && len(data) > 0 {
			var fileMap map[string]interface{}
			if json.Unmarshal(data, &fileMap) == nil && len(fileMap) > 0 {
				return fileMap, nil
			}
		}
	}

	if err != nil {
		return nil, err
	}
	return make(map[string]interface{}), nil
}

func (c *SRLClient) getRunningConfigOnBox(path string) (map[string]interface{}, error) {
	arg := "info from running"
	if path != "" && path != "/" {
		trimmed := strings.TrimPrefix(path, "/")
		arg = fmt.Sprintf("info from running %s", trimmed)
	}

	cmd := exec.Command("sr_cli", "-d", "--output-format", "json", arg)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err == nil && stdout.Len() > 0 {
		var result map[string]interface{}
		if json.Unmarshal(stdout.Bytes(), &result) == nil && len(result) > 0 {
			return result, nil
		}
	}

	// Fallback to startup config on disk during early boot
	if path == "" || path == "/" {
		if data, readErr := os.ReadFile("/etc/opt/srlinux/config.json"); readErr == nil && len(data) > 0 {
			var fileMap map[string]interface{}
			if json.Unmarshal(data, &fileMap) == nil && len(fileMap) > 0 {
				return fileMap, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to fetch config via on-box IPC: %s", stderr.String())
}

// GetSystemInfo queries basic switch inventory (hostname, version, chassis).
func (c *SRLClient) GetSystemInfo() map[string]string {
	info := map[string]string{
		"host-name": "srl-timeline",
		"version":   "SR Linux v26.7.1",
		"chassis":   "7220 IXR-D2L",
	}

	if c.IsOnBox {
		cmd := exec.Command("sr_cli", "-d", "--output-format", "json", "info from state system information")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err == nil && stdout.Len() > 0 {
			var stateMap map[string]interface{}
			if json.Unmarshal(stdout.Bytes(), &stateMap) == nil {
				if ver, ok := stateMap["version"].(string); ok && ver != "" {
					info["version"] = ver
				}
				if desc, ok := stateMap["description"].(string); ok && desc != "" {
					info["chassis"] = desc
				}
				if loc, ok := stateMap["location"].(string); ok && loc != "" {
					info["location"] = loc
				}
				return info
			}
		}
	}

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "get",
		"params": map[string]interface{}{
			"commands": []map[string]interface{}{
				{"path": "/system/name/host-name", "datastore": "running"},
				{"path": "/system/information", "datastore": "state"},
			},
		},
	}

	resp, err := c.postJSONRPC(payload)
	if err != nil {
		return info
	}

	if resList, ok := resp["result"].([]interface{}); ok {
		for _, item := range resList {
			if m, ok := item.(map[string]interface{}); ok {
				if hn, ok := m["host-name"].(string); ok && hn != "" {
					info["host-name"] = hn
				}
				if ver, ok := m["version"].(string); ok && ver != "" {
					info["version"] = ver
				}
				if ch, ok := m["chassis"].(string); ok && ch != "" {
					info["chassis"] = ch
				}
			}
		}
	}
	return info
}

// ReplaceFullConfig applies a full configuration replacement to the live switch.
func (c *SRLClient) ReplaceFullConfig(configDict map[string]interface{}) (bool, error) {
	if c.IsOnBox {
		// Use native SR Linux candidate JSON loading for maximum speed and atomic consistency
		tmpFile, err := os.CreateTemp("", "srl_restore_*.json")
		if err == nil {
			defer os.Remove(tmpFile.Name())
			jsonData, encErr := json.Marshal(configDict)
			if encErr == nil {
				_, writeErr := tmpFile.Write(jsonData)
				_ = tmpFile.Close()
				if writeErr == nil {
					commands := []string{
						fmt.Sprintf("load file %s", tmpFile.Name()),
					}
					ok, _, cliErr := c.execCLIOnBox(commands)
					if cliErr == nil && ok {
						return true, nil
					}
				}
			}
		}

		// Fallback to flat CLI statements
		cliStatements := normalizer.JSONToFlatCLI(configDict, "")
		if len(cliStatements) > 0 {
			var commands []string
			commands = append(commands, cliStatements...)
			ok, outMsg, err := c.execCLIOnBox(commands)
			if err == nil && ok {
				return true, nil
			}
			return false, fmt.Errorf("on-box CLI restore failed: %v (%s)", err, outMsg)
		}
	}

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "set",
		"params": map[string]interface{}{
			"commands": []map[string]interface{}{
				{
					"action": "replace",
					"path":   "/",
					"value":  configDict,
				},
			},
		},
	}

	resp, err := c.postJSONRPC(payload)
	if err != nil {
		return false, err
	}
	if errObj, ok := resp["error"].(map[string]interface{}); ok {
		return false, fmt.Errorf("JSON-RPC error: %v", errObj["message"])
	}
	return true, nil
}

// UpdateSubtreeConfig applies an update/replace to a specific subtree.
func (c *SRLClient) UpdateSubtreeConfig(path string, value interface{}) (bool, error) {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "set",
		"params": map[string]interface{}{
			"commands": []map[string]interface{}{
				{
					"action": "update",
					"path":   path,
					"value":  value,
				},
			},
		},
	}

	resp, err := c.postJSONRPC(payload)
	if err != nil {
		return false, err
	}
	if errObj, ok := resp["error"].(map[string]interface{}); ok {
		return false, fmt.Errorf("JSON-RPC error: %v", errObj["message"])
	}
	return true, nil
}

// ExecuteCLI executes a series of CLI commands on SR Linux.
func (c *SRLClient) ExecuteCLI(commands []string) (bool, string, error) {
	if len(commands) == 0 {
		return true, "", nil
	}

	if c.IsOnBox {
		return c.execCLIOnBox(commands)
	}

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      12,
		"method":  "cli",
		"params": map[string]interface{}{
			"commands": commands,
		},
	}

	resp, err := c.postJSONRPC(payload)
	if err != nil {
		return false, "", err
	}
	if errObj, ok := resp["error"].(map[string]interface{}); ok {
		return false, "", fmt.Errorf("CLI error: %v", errObj["message"])
	}

	var output strings.Builder
	if resList, ok := resp["result"].([]interface{}); ok {
		for _, item := range resList {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if txt, ok := itemMap["text"].(string); ok && txt != "" {
					output.WriteString(txt)
				}
			}
		}
	}

	return true, strings.TrimSpace(output.String()), nil
}

func (c *SRLClient) execCLIOnBox(commands []string) (bool, string, error) {
	var stdinBuf bytes.Buffer
	hasExplicitCandidate := false
	needsCandidate := false

	for _, cmd := range commands {
		t := strings.TrimSpace(cmd)
		if strings.HasPrefix(t, "enter candidate") {
			hasExplicitCandidate = true
		}
		if strings.HasPrefix(t, "set ") || strings.HasPrefix(t, "delete ") || strings.HasPrefix(t, "set /") || strings.HasPrefix(t, "delete /") || strings.HasPrefix(t, "load ") {
			needsCandidate = true
		}
	}

	if needsCandidate && !hasExplicitCandidate {
		stdinBuf.WriteString("enter candidate\n")
		for _, cmd := range commands {
			stdinBuf.WriteString(cmd)
			stdinBuf.WriteString("\n")
		}
		stdinBuf.WriteString("commit save\n")
		stdinBuf.WriteString("quit\n")
	} else {
		for _, cmd := range commands {
			stdinBuf.WriteString(cmd)
			stdinBuf.WriteString("\n")
		}
		stdinBuf.WriteString("quit\n")
	}

	cmd := exec.Command("sr_cli", "-d")
	cmd.Stdin = &stdinBuf
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	outStr := strings.TrimSpace(stdout.String())
	if err != nil {
		if stderr.Len() > 0 {
			return false, outStr, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
		}
		return false, outStr, err
	}
	return true, outStr, nil
}

// CommitLog represents an audit log entry from /var/log/srlinux/buffer/messages.
type CommitLog struct {
	Timestamp string `json:"timestamp"`
	Username  string `json:"username"`
	Session   string `json:"session"`
	Message   string `json:"message"`
}

// GetCommitLogs parses system messages to correlate authors and sessions.
func (c *SRLClient) GetCommitLogs(maxEntries int) []CommitLog {
	logPaths := []string{
		"/var/log/srlinux/buffer/messages",
		"/var/log/messages",
	}

	var rawContent []byte
	for _, p := range logPaths {
		data, err := os.ReadFile(p)
		if err == nil && len(data) > 0 {
			rawContent = data
			break
		}
	}

	if len(rawContent) == 0 {
		return []CommitLog{}
	}

	lines := strings.Split(string(rawContent), "\n")
	var entries []CommitLog

	// Example log: 2026-08-24T21:07:13.854301+00:00 srl-timeline sr_mgmt: Commit: commitSucceeded (username='root', session='22')
	logRegex := regexp.MustCompile(`^(\S+)\s+\S+\s+sr_mgmt.*commitSucceeded.*username='([^']+)'(?:.*session='([^']+)')?`)

	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if !strings.Contains(l, "commitSucceeded") {
			continue
		}
		matches := logRegex.FindStringSubmatch(l)
		if len(matches) >= 3 {
			ts := matches[1]
			user := matches[2]
			sess := ""
			if len(matches) >= 4 {
				sess = matches[3]
			}
			entries = append(entries, CommitLog{
				Timestamp: ts,
				Username:  user,
				Session:   sess,
				Message:   l,
			})
			if len(entries) >= maxEntries {
				break
			}
		}
	}
	return entries
}
