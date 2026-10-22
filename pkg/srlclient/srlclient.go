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

var (
	// Real SR Linux sr_mgmt_server log format:
	// 2026-10-22T18:00:05.644205+00:00 srl sr_mgmt_server: mgmt|3107|3107|00033|EV|commitSucceeded|I: All changes have been committed successfully by user root session 84.
	srlCommitLogRegex = regexp.MustCompile(`^(\S+)\s+\S+\s+(?:sr_mgmt|MGMT_SERVER)[^:]*:.*commitSucceeded.*by user\s+['"]?([^\s'"]+)['"]?.*?session\s+['"]?([^\s\.\)'"]+)`)
	// Legacy/synthetic log format:
	legacyCommitLogRegex = regexp.MustCompile(`^(\S+)\s+\S+\s+sr_mgmt.*commitSucceeded.*username='([^']+)'(?:.*session='([^']+)')?`)
	commitIDInLogRegex   = regexp.MustCompile(`commit-id\s+([^\s\]\)]+)`)
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
		// Execute curl inside srbase-mgmt namespace, passing credentials via stdin (--config -)
		// so passwords are never exposed in /proc/<pid>/cmdline.
		var cmdArgs []string
		if os.Geteuid() != 0 {
			cmdArgs = []string{"sudo", "-n", "ip", "netns", "exec", "srbase-mgmt"}
		} else {
			cmdArgs = []string{"ip", "netns", "exec", "srbase-mgmt"}
		}
		cmdArgs = append(cmdArgs,
			"curl", "-s",
			"--config", "-",
			"-H", "Content-Type: application/json",
			"-d", string(bodyBytes),
			c.JSONRPCURL,
		)
		curlCmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		var curlConfig bytes.Buffer
		if c.Username != "" {
			escapedUser := strings.ReplaceAll(c.Username, "\"", "\\\"")
			escapedPass := strings.ReplaceAll(c.Password, "\"", "\\\"")
			curlConfig.WriteString(fmt.Sprintf("user = \"%s:%s\"\n", escapedUser, escapedPass))
		}
		curlCmd.Stdin = &curlConfig
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
				if norm, ok := normalizer.NormalizeStructure(resMap, false).(map[string]interface{}); ok {
					return norm, nil
				}
				return resMap, nil
			}
		}
	}

	// Fallback to startup config file on disk only when off-box
	if path == "/" && !c.IsOnBox {
		if data, readErr := os.ReadFile("/etc/opt/srlinux/config.json"); readErr == nil && len(data) > 0 {
			var fileMap map[string]interface{}
			if json.Unmarshal(data, &fileMap) == nil && len(fileMap) > 0 {
				if norm, ok := normalizer.NormalizeStructure(fileMap, false).(map[string]interface{}); ok {
					return norm, nil
				}
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
			if norm, ok := normalizer.NormalizeStructure(result, false).(map[string]interface{}); ok {
				return norm, nil
			}
			return result, nil
		}
	}

	// Fallback to startup config on disk during early boot
	if path == "" || path == "/" {
		if data, readErr := os.ReadFile("/etc/opt/srlinux/config.json"); readErr == nil && len(data) > 0 {
			var fileMap map[string]interface{}
			if json.Unmarshal(data, &fileMap) == nil && len(fileMap) > 0 {
				if norm, ok := normalizer.NormalizeStructure(fileMap, false).(map[string]interface{}); ok {
					return norm, nil
				}
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

	if hn, err := os.Hostname(); err == nil && hn != "" {
		info["host-name"] = hn
	}

	if c.IsOnBox {
		cmd := exec.Command("sr_cli", "-d", "--output-format", "json", "info from state system information")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err == nil && stdout.Len() > 0 {
			var stateMap map[string]interface{}
			if json.Unmarshal(stdout.Bytes(), &stateMap) == nil {
				if sys, ok := stateMap["system"].(map[string]interface{}); ok {
					if inf, ok := sys["information"].(map[string]interface{}); ok {
						stateMap = inf
					}
				} else if inf, ok := stateMap["information"].(map[string]interface{}); ok {
					stateMap = inf
				}
				if ver, ok := stateMap["version"].(string); ok && ver != "" {
					info["version"] = ver
				}
				if desc, ok := stateMap["description"].(string); ok && desc != "" {
					info["chassis"] = desc
				} else if ch, ok := stateMap["chassis"].(string); ok && ch != "" {
					info["chassis"] = ch
				}
				if loc, ok := stateMap["location"].(string); ok && loc != "" {
					info["location"] = loc
				}
			}
		}

		hnCmd := exec.Command("sr_cli", "-d", "--output-format", "json", "info from state system name")
		var hnOut bytes.Buffer
		hnCmd.Stdout = &hnOut
		if err := hnCmd.Run(); err == nil && hnOut.Len() > 0 {
			var nameMap map[string]interface{}
			if json.Unmarshal(hnOut.Bytes(), &nameMap) == nil {
				if sys, ok := nameMap["system"].(map[string]interface{}); ok {
					if nm, ok := sys["name"].(map[string]interface{}); ok {
						nameMap = nm
					}
				} else if nm, ok := nameMap["name"].(map[string]interface{}); ok {
					nameMap = nm
				}
				if hn, ok := nameMap["host-name"].(string); ok && hn != "" {
					info["host-name"] = hn
				}
			}
		}
		return info
	}

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "get",
		"params": map[string]interface{}{
			"commands": []map[string]interface{}{
				{"path": "/system/name/host-name", "datastore": "state"},
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
			switch v := item.(type) {
			case string:
				if v != "" {
					info["host-name"] = v
				}
			case map[string]interface{}:
				if hn, ok := v["host-name"].(string); ok && hn != "" {
					info["host-name"] = hn
				}
				if ver, ok := v["version"].(string); ok && ver != "" {
					info["version"] = ver
				}
				if desc, ok := v["description"].(string); ok && desc != "" {
					info["chassis"] = desc
				} else if ch, ok := v["chassis"].(string); ok && ch != "" {
					info["chassis"] = ch
				}
			}
		}
	}
	return info
}

// ReplaceFullConfig applies a full configuration replacement to the live switch.
func (c *SRLClient) ReplaceFullConfig(configDict map[string]interface{}) (bool, error) {
	cleanDict, _ := normalizer.NormalizeStructure(configDict, false).(map[string]interface{})
	if cleanDict == nil {
		cleanDict = configDict
	}

	if c.IsOnBox {
		// Use native SR Linux candidate JSON loading with explicit "delete /" first so it replaces rather than merges
		tmpFile, err := os.CreateTemp("", "srl_restore_*.json")
		if err == nil {
			defer os.Remove(tmpFile.Name())
			jsonData, encErr := json.Marshal(cleanDict)
			if encErr == nil {
				_, writeErr := tmpFile.Write(jsonData)
				_ = tmpFile.Close()
				if writeErr == nil {
					commands := []string{
						"delete /",
						fmt.Sprintf("load file %s", tmpFile.Name()),
					}
					ok, _, cliErr := c.execCLIOnBox(commands)
					if cliErr == nil && ok {
						return true, nil
					}
				}
			}
		}

		// Fallback to flat CLI statements preceded by "delete /" for atomic replacement
		cliStatements := normalizer.JSONToFlatCLI(cleanDict, "")
		if len(cliStatements) > 0 {
			commands := make([]string, 0, len(cliStatements)+1)
			commands = append(commands, "delete /")
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
					"value":  cleanDict,
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

// unescapeCLIMultilineCommand converts Go-escaped \n, \r, and \t sequences inside double-quoted
// CLI string arguments back to literal characters so sr_cli -d preserves multiline PEM certificates and banners.
func unescapeCLIMultilineCommand(cmd string) string {
	if !strings.Contains(cmd, `\`) || !strings.Contains(cmd, `"`) {
		return cmd
	}
	var sb strings.Builder
	sb.Grow(len(cmd))
	inQuote := false
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		if ch == '"' {
			inQuote = !inQuote
			sb.WriteByte(ch)
			continue
		}
		if inQuote && ch == '\\' && i+1 < len(cmd) {
			next := cmd[i+1]
			switch next {
			case 'n':
				sb.WriteByte('\n')
				i++
			case 'r':
				sb.WriteByte('\r')
				i++
			case 't':
				sb.WriteByte('\t')
				i++
			default:
				sb.WriteByte('\\')
				sb.WriteByte(next)
				i++
			}
			continue
		}
		sb.WriteByte(ch)
	}
	return sb.String()
}

func (c *SRLClient) execCLIOnBox(commands []string) (bool, string, error) {
	var stdinBuf bytes.Buffer
	needsCandidate := false
	var bodyCommands []string

	for _, cmd := range commands {
		t := strings.TrimSpace(cmd)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "enter candidate") {
			needsCandidate = true
			continue
		}
		if t == "commit save" || t == "commit now" || t == "commit stay" || t == "quit" || t == "discard stay" || t == "discard now" {
			continue
		}
		if strings.HasPrefix(t, "set ") || strings.HasPrefix(t, "delete ") || strings.HasPrefix(t, "set /") || strings.HasPrefix(t, "delete /") || strings.HasPrefix(t, "load ") {
			needsCandidate = true
		}
		bodyCommands = append(bodyCommands, unescapeCLIMultilineCommand(cmd))
	}

	if needsCandidate {
		stdinBuf.WriteString("enter candidate private name timeline-tx\n")
		stdinBuf.WriteString("discard stay\n")
		for _, cmd := range bodyCommands {
			stdinBuf.WriteString(cmd)
			stdinBuf.WriteString("\n")
		}
		stdinBuf.WriteString("commit save\n")
		stdinBuf.WriteString("quit\n")
	} else {
		for _, cmd := range bodyCommands {
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
		if needsCandidate {
			// sr_cli -d aborts reading stdin on the first failing line; run an explicit cleanup session
			// so uncommitted lines never pollute the candidate datastore.
			cleanupCmd := exec.Command("sr_cli", "-d")
			cleanupCmd.Stdin = strings.NewReader("enter candidate private name timeline-tx\ndiscard now\nquit\n")
			_ = cleanupCmd.Run()
		}
		if stderr.Len() > 0 {
			return false, outStr, fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
		}
		return false, outStr, err
	}
	return true, outStr, nil
}

// CommitLog represents an audit log entry from /var/log/srlinux/buffer/messages or state datastore.
type CommitLog struct {
	Timestamp string `json:"timestamp"`
	Username  string `json:"username"`
	Session   string `json:"session"`
	CommitID  string `json:"commit_id,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Message   string `json:"message"`
}

// ParseCommitLogLine parses a single syslog line for a commitSucceeded event.
func ParseCommitLogLine(line string) (CommitLog, bool) {
	if !strings.Contains(line, "commitSucceeded") && !(strings.Contains(line, "commit") && strings.Contains(line, "succeeded")) {
		return CommitLog{}, false
	}
	cid := ""
	if cm := commitIDInLogRegex.FindStringSubmatch(line); len(cm) >= 2 {
		cid = cm[1]
	}
	if m := srlCommitLogRegex.FindStringSubmatch(line); len(m) >= 4 {
		return CommitLog{
			Timestamp: m[1],
			Username:  m[2],
			Session:   strings.TrimRight(m[3], "."),
			CommitID:  cid,
			Message:   line,
		}, true
	}
	if m := legacyCommitLogRegex.FindStringSubmatch(line); len(m) >= 3 {
		sess := ""
		if len(m) >= 4 {
			sess = m[3]
		}
		return CommitLog{
			Timestamp: m[1],
			Username:  m[2],
			Session:   sess,
			CommitID:  cid,
			Message:   line,
		}, true
	}
	return CommitLog{}, false
}

// GetLatestCommitState queries /system/configuration/commit in the SR Linux state datastore
// to retrieve the latest commit ID, comment, username, and completion timestamp.
func (c *SRLClient) GetLatestCommitState() (CommitLog, bool) {
	var stateMap map[string]interface{}

	if c.IsOnBox {
		cmd := exec.Command("sr_cli", "-d", "--output-format", "json", "info from state system configuration commit *")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err == nil && stdout.Len() > 0 {
			_ = json.Unmarshal(stdout.Bytes(), &stateMap)
		}
	} else if c.UseNetNS || (c.JSONRPCURL != "" && c.JSONRPCURL != "http://127.0.0.1:80/jsonrpc") {
		payload := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      20,
			"method":  "get",
			"params": map[string]interface{}{
				"commands": []map[string]interface{}{
					{"path": "/system/configuration/commit", "datastore": "state"},
				},
			},
		}
		if resp, err := c.postJSONRPC(payload); err == nil {
			if resList, ok := resp["result"].([]interface{}); ok && len(resList) > 0 {
				stateMap, _ = resList[0].(map[string]interface{})
			}
		}
	}

	return parseLatestCommitState(stateMap)
}

func parseLatestCommitState(stateMap map[string]interface{}) (CommitLog, bool) {
	if len(stateMap) == 0 {
		return CommitLog{}, false
	}

	if sys, ok := stateMap["system"].(map[string]interface{}); ok {
		if cfg, ok := sys["configuration"].(map[string]interface{}); ok {
			stateMap = cfg
		}
	} else if cfg, ok := stateMap["configuration"].(map[string]interface{}); ok {
		stateMap = cfg
	}

	commitList, ok := stateMap["commit"].([]interface{})
	if !ok || len(commitList) == 0 {
		return CommitLog{}, false
	}

	// Select the latest complete user commit (or latest complete commit if all are __internal__)
	var chosen map[string]interface{}
	for i := len(commitList) - 1; i >= 0; i-- {
		m, ok := commitList[i].(map[string]interface{})
		if !ok {
			continue
		}
		if status, _ := m["status"].(string); status != "" && status != "complete" {
			continue
		}
		if chosen == nil {
			chosen = m
		}
		user, _ := m["username"].(string)
		if user != "" && user != "__internal__" {
			chosen = m
			break
		}
	}
	if chosen == nil {
		return CommitLog{}, false
	}

	var out CommitLog
	if idVal, exists := chosen["id"]; exists && idVal != nil {
		out.CommitID = fmt.Sprintf("%v", idVal)
	}
	if user, _ := chosen["username"].(string); user != "" && user != "__internal__" {
		out.Username = user
	}
	if comment, _ := chosen["comment"].(string); comment != "" {
		out.Comment = comment
	}
	if ended, _ := chosen["ended"].(string); ended != "" {
		fields := strings.Fields(ended)
		if len(fields) > 0 {
			out.Timestamp = fields[0]
		}
	}
	return out, out.CommitID != "" || out.Username != "" || out.Comment != ""
}

// GetCommitLogs parses system messages and enriches with /system/configuration/commit state.
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

	var entries []CommitLog
	if len(rawContent) > 0 {
		lines := strings.Split(string(rawContent), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if entry, ok := ParseCommitLogLine(lines[i]); ok {
				entries = append(entries, entry)
				if len(entries) >= maxEntries {
					break
				}
			}
		}
	}

	if stateCommit, ok := c.GetLatestCommitState(); ok {
		if len(entries) > 0 {
			if entries[0].CommitID == "" && stateCommit.CommitID != "" {
				entries[0].CommitID = stateCommit.CommitID
			}
			if entries[0].Comment == "" && stateCommit.Comment != "" {
				entries[0].Comment = stateCommit.Comment
			}
			if (entries[0].Username == "" || entries[0].Username == "admin") && stateCommit.Username != "" {
				entries[0].Username = stateCommit.Username
			}
		} else {
			entries = append(entries, stateCommit)
		}
	}

	return entries
}
