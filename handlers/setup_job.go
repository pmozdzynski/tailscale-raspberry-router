package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const setupProgressFile = "/run/tailscale-router/setup-progress.json"

type SetupStepLine struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	At     string `json:"at"`
}

type SetupJobProgress struct {
	Running    bool            `json:"running"`
	Complete   bool            `json:"complete"`
	Error      string          `json:"error,omitempty"`
	Message    string          `json:"message,omitempty"`
	Configured bool            `json:"configured"`
	Lines      []SetupStepLine `json:"lines"`
}

var (
	setupJobMu      sync.Mutex
	setupJobRunning bool
)

func loadProgress() SetupJobProgress {
	data, err := os.ReadFile(setupProgressFile)
	if err != nil {
		return SetupJobProgress{}
	}
	var p SetupJobProgress
	if json.Unmarshal(data, &p) != nil {
		return SetupJobProgress{}
	}
	return p
}

func saveProgress(p SetupJobProgress) {
	_ = os.MkdirAll(filepath.Dir(setupProgressFile), 0755)
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	_ = os.WriteFile(setupProgressFile, data, 0644)
}

func setupCompleteMessage() string {
	ips := getManagementAccessIPs()
	if len(ips) > 0 {
		return "Dashboard login: " + RouterLoginURL(ips[0])
	}
	return "Dashboard on port " + RouterPort
}

func GetSetupJobProgress() SetupJobProgress {
	setupJobMu.Lock()
	running := setupJobRunning
	setupJobMu.Unlock()

	p := loadProgress()
	if running {
		p.Running = true
	}
	p.Configured = IsConfigured()
	if p.Configured && !p.Complete && p.Error == "" {
		p.Complete = true
		if p.Message == "" {
			p.Message = setupCompleteMessage()
		}
	}
	return p
}

func ClearSetupProgress() {
	setupJobMu.Lock()
	setupJobRunning = false
	setupJobMu.Unlock()
	_ = os.Remove(setupProgressFile)
}

func tryStartSetupJob() bool {
	setupJobMu.Lock()
	defer setupJobMu.Unlock()
	if setupJobRunning {
		return false
	}
	setupJobRunning = true
	saveProgress(SetupJobProgress{Running: true, Lines: []SetupStepLine{}})
	return true
}

func isSetupJobRunning() bool {
	setupJobMu.Lock()
	defer setupJobMu.Unlock()
	return setupJobRunning
}

func recordSetupProgress(status, step, detail string) {
	p := loadProgress()
	p.Running = true
	p.Lines = append(p.Lines, SetupStepLine{
		Step:   step,
		Status: status,
		Detail: detail,
		At:     time.Now().Format("15:04:05"),
	})
	saveProgress(p)
}

func finishSetupJob(err error, message string) {
	setupJobMu.Lock()
	setupJobRunning = false
	setupJobMu.Unlock()

	p := loadProgress()
	p.Running = false
	p.Complete = err == nil
	p.Message = message
	if err != nil {
		p.Error = err.Error()
	}
	p.Configured = IsConfigured()
	saveProgress(p)
}

func setupProgressReporterFromJob() setupProgressReporter {
	return setupProgressReporter(func(status, step, detail string) {
		recordSetupProgress(status, step, detail)
	})
}

func startSetupJobAsync(cfg RouterConfig, authKey string) {
	go func() {
		err := ApplyBootstrapWithProgress(cfg, authKey, setupProgressReporterFromJob())
		if err != nil {
			finishSetupJob(err, "")
			return
		}
		finishSetupJob(nil, setupCompleteMessage())
	}()
}
