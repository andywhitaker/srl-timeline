package srlclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"timeline/pkg/normalizer"
)

const (
	// DefaultUnixSocketPath is the native SR Linux JSON-RPC Unix domain socket.
	DefaultUnixSocketPath = "/opt/srlinux/var/run/sr_json_rpc"
	// DefaultMgmtNetNSPath is the SR Linux management network namespace path.
	DefaultMgmtNetNSPath = "/var/run/netns/srbase-mgmt"
)

var (
	// Real SR Linux sr_mgmt_server log format (both 24.x EV|commitSucceeded and 25.10+ I: All changes have been committed successfully):
	// 2026-10-22T18:00:05.644205+00:00 srl sr_mgmt_server: mgmt|3107|3107|00033|EV|commitSucceeded|I: All changes have been committed successfully by user root session 84.
	// 2026-10-25T20:15:57.195804+00:00 srl1 sr_mgmt_server: mgmt|2882|2882|00024|I: All changes have been committed successfully by user root session 19.
	srlCommitLogRegex = regexp.MustCompile(`^(\S+)\s+\S+\s+(?:sr_mgmt|MGMT_SERVER)[^:]*:.*(?:commitSucceeded|committed successfully).*by user\s+['"]?([^\s'"]+)['"]?.*?session\s+['"]?([^\s\.\)'"]+)`)
	// Legacy/synthetic log format:
	legacyCommitLogRegex = regexp.MustCompile(`^(\S+)\s+\S+\s+sr_mgmt.*commitSucceeded.*username='([^']+)'(?:.*session='([^']+)')?`)
	commitIDInLogRegex   = regexp.MustCompile(`commit-id\s+([^\s\]\)]+)`)
)

// SRLClient manages realtime interactions with Nokia SR Linux services via native JSON-RPC.
type SRLClient struct {
	JSONRPCURL     string
	UnixSocketPath string
	Username       string
	Password       string
	UseNetNS       bool
	IsOnBox        bool
	HTTPClient     *http.Client
	UnixClient     *http.Client
}

// NewSRLClient creates a new SRLClient.
func NewSRLClient() *SRLClient {
	url := os.Getenv("SRL_JSONRPC_URL")
	unixSock := os.Getenv("SRL_UNIX_SOCKET")
	user := os.Getenv("SRL_USER")
	pass := os.Getenv("SRL_PASS")

	// Detect if running on an SR Linux switch
	isOnBox := false
	for _, p := range []string{
		"/opt/srlinux/var/run/sr_mgmt_server:50051",
		"/opt/srlinux/var/run/sr_grpc_server_mgmt",
		"/opt/srlinux/bin/sr_cli",
		"/etc/opt/srlinux",
	} {
		if _, err := os.Stat(p); err == nil {
			isOnBox = true
			break
		}
	}

	useNetNS := false
	if _, err := os.Stat(DefaultMgmtNetNSPath); err == nil {
		useNetNS = true
	}

	if url == "" {
		url = "http://127.0.0.1:80/jsonrpc"
	}
	if unixSock == "" && isOnBox {
		unixSock = DefaultUnixSocketPath
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if useNetNS && os.Geteuid() == 0 {
				if conn, err := dialInNetNS(ctx, DefaultMgmtNetNSPath, network, addr); err == nil {
					return conn, nil
				}
			}
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}

	c := &SRLClient{
		JSONRPCURL:     url,
		UnixSocketPath: unixSock,
		Username:       user,
		Password:       pass,
		UseNetNS:       useNetNS,
		IsOnBox:        isOnBox,
		HTTPClient:     &http.Client{Transport: tr, Timeout: 15 * time.Second},
	}

	c.UnixClient = &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				sockPath := c.UnixSocketPath
				if sockPath == "" {
					sockPath = DefaultUnixSocketPath
				}
				var d net.Dialer
				return d.DialContext(ctx, "unix", sockPath)
			},
		},
		Timeout: 15 * time.Second,
	}

	return c
}

// dialInNetNS dials a network connection inside the specified network namespace in pure Go
// by locking the OS thread, switching network namespaces via setns(2), dialing, and restoring the original netns.
func dialInNetNS(ctx context.Context, netnsPath, network, addr string) (net.Conn, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origNS, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, err
	}
	defer origNS.Close()

	targetNS, err := os.Open(netnsPath)
	if err != nil {
		return nil, err
	}
	defer targetNS.Close()

	const cloneNewNet = 0x40000000 // CLONE_NEWNET
	sysSetns := uintptr(308)       // SYS_SETNS on linux/amd64
	if runtime.GOARCH == "arm64" {
		sysSetns = 268
	}
	if _, _, errno := syscall.Syscall(sysSetns, targetNS.Fd(), cloneNewNet, 0); errno != 0 {
		return nil, errno
	}
	defer func() {
		_, _, _ = syscall.Syscall(sysSetns, origNS.Fd(), cloneNewNet, 0)
	}()

	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func (c *SRLClient) hasExplicitRemoteURL() bool {
	if envURL := os.Getenv("SRL_JSONRPC_URL"); envURL != "" {
		return true
	}
	return c.JSONRPCURL != "" && c.JSONRPCURL != "http://127.0.0.1:80/jsonrpc"
}

func (c *SRLClient) shouldUseUnixSocket() bool {
	if os.Getenv("SRL_UNIX_SOCKET") != "" {
		return true
	}
	return c.IsOnBox && !c.hasExplicitRemoteURL()
}

func (c *SRLClient) postUnixJSONRPC(bodyBytes []byte) (map[string]interface{}, error) {
	sockPath := c.UnixSocketPath
	if sockPath == "" {
		sockPath = DefaultUnixSocketPath
	}
	info, err := os.Stat(sockPath)
	if err != nil {
		return nil, err
	}
	if os.Geteuid() == 0 && info.Mode().Perm()&0006 != 0006 {
		_ = os.Chmod(sockPath, 0777)
	}

	client := c.UnixClient
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", sockPath)
				},
			},
			Timeout: 15 * time.Second,
		}
	}

	req, err := http.NewRequest("POST", "http://unix/jsonrpc", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	if c.Username != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
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

func (c *SRLClient) postTCPJSONRPC(bodyBytes []byte) (map[string]interface{}, error) {
	targetURL := c.JSONRPCURL
	if targetURL == "" {
		targetURL = "http://127.0.0.1:80/jsonrpc"
	}

	req, err := http.NewRequest("POST", targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	user := c.Username
	pass := c.Password
	if user == "" && c.IsOnBox && !c.hasExplicitRemoteURL() {
		user = "admin"
		pass = "NokiaSrl1!"
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := client.Do(req)
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

func (c *SRLClient) enableUnixSocketViaTCP() error {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "set",
		"params": map[string]interface{}{
			"commands": []map[string]interface{}{
				{
					"action": "update",
					"path":   "/system/json-rpc-server/unix-socket",
					"value": map[string]interface{}{
						"admin-state":        "enable",
						"use-authentication": false,
					},
				},
			},
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := c.postTCPJSONRPC(bodyBytes)
	if err != nil {
		return err
	}
	if errObj, ok := resp["error"].(map[string]interface{}); ok {
		return fmt.Errorf("JSON-RPC error: %v", errObj["message"])
	}

	sockPath := c.UnixSocketPath
	if sockPath == "" {
		sockPath = DefaultUnixSocketPath
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(sockPath); statErr == nil {
			_ = os.Chmod(sockPath, 0777)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("unix socket %s did not appear after enabling", sockPath)
}

func (c *SRLClient) postJSONRPC(payload map[string]interface{}) (map[string]interface{}, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	if c.shouldUseUnixSocket() {
		if resp, err := c.postUnixJSONRPC(bodyBytes); err == nil {
			return resp, nil
		}
		if os.Geteuid() == 0 {
			if bootErr := c.enableUnixSocketViaTCP(); bootErr == nil {
				if resp, err := c.postUnixJSONRPC(bodyBytes); err == nil {
					return resp, nil
				}
			}
		}
	}

	return c.postTCPJSONRPC(bodyBytes)
}

// EnsureUnixSocket enables the on-box JSON-RPC Unix domain socket and sets shared permissions.
func (c *SRLClient) EnsureUnixSocket() error {
	sockPath := c.UnixSocketPath
	if sockPath == "" {
		sockPath = DefaultUnixSocketPath
	}

	if _, err := os.Stat(sockPath); err != nil {
		if bootErr := c.enableUnixSocketViaTCP(); bootErr != nil {
			return bootErr
		}
	}
	if _, err := os.Stat(sockPath); err == nil {
		_ = os.Chmod(sockPath, 0777)
	}
	return nil
}

// BootstrapOnBox enables the on-box JSON-RPC Unix domain socket, sets shared permissions,
// saves startup configuration, and reloads app_mgr via native JSON-RPC tools datastore.
func (c *SRLClient) BootstrapOnBox() error {
	_ = c.EnsureUnixSocket()
	_, _ = c.SaveStartupConfig()
	_, reloadErr := c.ReloadAppMgr()
	return reloadErr
}

// GetRunningConfig retrieves the running configuration (or a subtree) via native JSON-RPC get.
func (c *SRLClient) GetRunningConfig(path string) (map[string]interface{}, error) {
	if path == "" {
		path = "/"
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
	if err != nil {
		return nil, err
	}
	if errObj, ok := resp["error"].(map[string]interface{}); ok {
		return nil, fmt.Errorf("JSON-RPC error: %v", errObj["message"])
	}

	if resList, ok := resp["result"].([]interface{}); ok && len(resList) > 0 {
		if resMap, ok := resList[0].(map[string]interface{}); ok && len(resMap) > 0 {
			if norm, ok := normalizer.NormalizeStructure(resMap, false).(map[string]interface{}); ok {
				return norm, nil
			}
			return resMap, nil
		}
	}

	return make(map[string]interface{}), nil
}

// GetSystemInfo queries basic switch inventory (hostname, version, chassis) from the state datastore.
func (c *SRLClient) GetSystemInfo() map[string]string {
	info := map[string]string{
		"host-name": "srl-timeline",
		"version":   "SR Linux v26.7.1",
		"chassis":   "7220 IXR-D2L",
	}

	if hn, err := os.Hostname(); err == nil && hn != "" {
		info["host-name"] = hn
	}

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "get",
		"params": map[string]interface{}{
			"commands": []map[string]interface{}{
				{"path": "/system/name/host-name", "datastore": "state"},
				{"path": "/system/information", "datastore": "state"},
				{"path": "/platform/chassis/type", "datastore": "state"},
			},
		},
	}

	resp, err := c.postJSONRPC(payload)
	if err != nil {
		return info
	}

	if resList, ok := resp["result"].([]interface{}); ok {
		for idx, item := range resList {
			switch v := item.(type) {
			case string:
				if v != "" {
					if idx == 2 {
						info["chassis"] = v
					} else {
						info["host-name"] = v
					}
				}
			case map[string]interface{}:
				if hn, ok := v["host-name"].(string); ok && hn != "" {
					info["host-name"] = hn
				}
				if ver, ok := v["version"].(string); ok && ver != "" {
					info["version"] = ver
				}
				if ch, ok := v["chassis"].(string); ok && ch != "" {
					info["chassis"] = ch
				} else if tp, ok := v["type"].(string); ok && tp != "" {
					info["chassis"] = tp
				}
				if loc, ok := v["location"].(string); ok && loc != "" {
					info["location"] = loc
				}
			}
		}
	}
	return info
}

// ReplaceFullConfig applies a full configuration replacement to the live switch via native JSON-RPC set.
func (c *SRLClient) ReplaceFullConfig(configDict map[string]interface{}) (bool, error) {
	cleanDict, _ := normalizer.NormalizeStructure(configDict, false).(map[string]interface{})
	if cleanDict == nil {
		cleanDict = configDict
	}

	commands := []map[string]interface{}{
		{
			"action": "replace",
			"path":   "/",
			"value":  cleanDict,
		},
	}

	if c.shouldUseUnixSocket() {
		commands = append(commands, map[string]interface{}{
			"action": "update",
			"path":   "/system/json-rpc-server/unix-socket",
			"value": map[string]interface{}{
				"admin-state":        "enable",
				"use-authentication": false,
			},
		})
	}

	return c.ApplyConfigCommands(commands)
}

// UpdateSubtreeConfig applies an update to a specific subtree via native JSON-RPC set.
func (c *SRLClient) UpdateSubtreeConfig(path string, value interface{}) (bool, error) {
	commands := []map[string]interface{}{
		{
			"action": "update",
			"path":   path,
			"value":  value,
		},
	}
	return c.ApplyConfigCommands(commands)
}

// ApplyConfigCommands executes a batch of candidate datastore commands (delete/update/replace) atomically.
func (c *SRLClient) ApplyConfigCommands(commands []map[string]interface{}) (bool, error) {
	if len(commands) == 0 {
		return true, nil
	}

	if c.shouldUseUnixSocket() {
		needsUnixRestore := false
		hasUnixUpdate := false
		for _, cmd := range commands {
			act, _ := cmd["action"].(string)
			p, _ := cmd["path"].(string)
			if act == "delete" && (p == "/" || p == "/system" || strings.HasPrefix(p, "/system/json-rpc-server")) {
				needsUnixRestore = true
			}
			if act == "update" && p == "/system/json-rpc-server/unix-socket" {
				hasUnixUpdate = true
			}
		}
		if needsUnixRestore && !hasUnixUpdate {
			commands = append(append([]map[string]interface{}{}, commands...), map[string]interface{}{
				"action": "update",
				"path":   "/system/json-rpc-server/unix-socket",
				"value": map[string]interface{}{
					"admin-state":        "enable",
					"use-authentication": false,
				},
			})
		}
	}

	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "set",
		"params": map[string]interface{}{
			"commands": commands,
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

// SaveStartupConfig invokes /system/configuration/save in the SR Linux tools datastore.
func (c *SRLClient) SaveStartupConfig() (bool, error) {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      15,
		"method":  "set",
		"params": map[string]interface{}{
			"datastore": "tools",
			"commands": []map[string]interface{}{
				{
					"action": "update",
					"path":   "/system/configuration/save",
					"value":  map[string]interface{}{},
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

// ReloadAppMgr invokes /system/app-management/application[name=app_mgr]/reload in the SR Linux tools datastore.
func (c *SRLClient) ReloadAppMgr() (bool, error) {
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      16,
		"method":  "set",
		"params": map[string]interface{}{
			"datastore": "tools",
			"commands": []map[string]interface{}{
				{
					"action": "update",
					"path":   "/system/app-management/application[name=app_mgr]/reload",
					"value":  map[string]interface{}{},
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

// CommitLog represents an audit log entry from /system/configuration/commit state or system messages.
type CommitLog struct {
	Timestamp string `json:"timestamp"`
	Username  string `json:"username"`
	Session   string `json:"session"`
	CommitID  string `json:"commit_id,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Message   string `json:"message"`
}

// ParseCommitLogLine parses a single syslog line for a commitSucceeded or committed successfully event.
func ParseCommitLogLine(line string) (CommitLog, bool) {
	if !strings.Contains(line, "commitSucceeded") &&
		!strings.Contains(line, "committed successfully") &&
		!(strings.Contains(line, "commit") && strings.Contains(line, "succeeded")) {
		return CommitLog{}, false
	}
	cid := ""
	if cm := commitIDInLogRegex.FindStringSubmatch(line); len(cm) >= 2 {
		cid = cm[1]
	}
	if m := srlCommitLogRegex.FindStringSubmatch(line); len(m) >= 4 {
		user := m[2]
		if user == "__internal__" || user == "__jsonrpc__" {
			user = "admin"
		}
		return CommitLog{
			Timestamp: m[1],
			Username:  user,
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
		user := m[2]
		if user == "__internal__" || user == "__jsonrpc__" {
			user = "admin"
		}
		return CommitLog{
			Timestamp: m[1],
			Username:  user,
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
	if !c.IsOnBox && !c.UseNetNS && !c.hasExplicitRemoteURL() {
		return CommitLog{}, false
	}

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

	var stateMap map[string]interface{}
	if resp, err := c.postJSONRPC(payload); err == nil {
		if resList, ok := resp["result"].([]interface{}); ok && len(resList) > 0 {
			stateMap, _ = resList[0].(map[string]interface{})
		}
	}

	return parseLatestCommitState(stateMap)
}

func isInternalUsername(user string) bool {
	return user == "" || user == "__internal__" || user == "__jsonrpc__"
}

func parseLatestCommitState(stateMap map[string]interface{}) (CommitLog, bool) {
	if len(stateMap) == 0 {
		return CommitLog{}, false
	}
	if norm, ok := normalizer.NormalizeStructure(stateMap, false).(map[string]interface{}); ok {
		stateMap = norm
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

	// Select the complete commit with the highest numeric ID (preferring non-__internal__ commits at the highest ID)
	var bestUser map[string]interface{}
	bestUserID := -1
	var bestAny map[string]interface{}
	bestAnyID := -1

	for i, item := range commitList {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if status, _ := m["status"].(string); status != "" && status != "complete" {
			continue
		}
		cid := i
		if idVal, exists := m["id"]; exists && idVal != nil {
			if parsed, err := strconv.Atoi(fmt.Sprintf("%v", idVal)); err == nil {
				cid = parsed
			}
		}
		if bestAny == nil || cid >= bestAnyID {
			bestAny = m
			bestAnyID = cid
		}
		user, _ := m["username"].(string)
		if user != "__internal__" {
			if bestUser == nil || cid >= bestUserID {
				bestUser = m
				bestUserID = cid
			}
		}
	}

	chosen := bestUser
	if chosen == nil || bestAnyID > bestUserID {
		chosen = bestAny
	}
	if chosen == nil {
		return CommitLog{}, false
	}

	var out CommitLog
	if idVal, exists := chosen["id"]; exists && idVal != nil {
		out.CommitID = fmt.Sprintf("%v", idVal)
	}
	if user, _ := chosen["username"].(string); !isInternalUsername(user) {
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

// GetCommitLogs queries /system/configuration/commit in the state datastore as the primary source
// and falls back to parsing system log files only when needed.
func (c *SRLClient) GetCommitLogs(maxEntries int) []CommitLog {
	if stateCommit, ok := c.GetLatestCommitState(); ok && maxEntries <= 1 {
		return []CommitLog{stateCommit}
	}

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
			if stateCommit.CommitID != "" {
				entries[0].CommitID = stateCommit.CommitID
			}
			if stateCommit.Comment != "" {
				entries[0].Comment = stateCommit.Comment
			}
			if stateCommit.Username != "" {
				entries[0].Username = stateCommit.Username
			}
			if stateCommit.Timestamp != "" {
				entries[0].Timestamp = stateCommit.Timestamp
			}
		} else {
			entries = append(entries, stateCommit)
		}
	}

	return entries
}
