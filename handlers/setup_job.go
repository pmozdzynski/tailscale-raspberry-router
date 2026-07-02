package handlers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const setupProgressFile = "/run/tailscale-router/setup-progress.json"

// SetupStepLine is one line in the setup install log.
type SetupStepLine struct {
	Step   string `json:"step"`
	Status string `json:"status"` // running, ok, warn, error
	Detail string `json:"detail"`
	At     string `json:"at"`
}

// SetupJobProgress is returned by GET /setup/progress.
type SetupJobProgress struct {
	Running    bool            `json:"running"`
	Complete   bool            `json:"complete"`
	Error      string          `json:"error,omitempty"`
	Message    string          `json:"message,omitempty"`
	Configured bool            `json:"configured"`
	UpdatedAt  string          `json:"updated_at"`
	Lines      []SetupStepLine `json:"lines"`
}

var (
	setupJobMu      sync.Mutex
	setupJobRunning bool
	setupJobLines   []SetupStepLine
	setupJobError   string
	setupJobMessage string
	setupJobDone    bool
)

func finishSetupJob(err error, message string) {
	setupJobMu.Lock()
	defer setupJobMu.Unlock()
	setupJobRunning = false
	setupJobDone = true
	setupJobMessage = message
	if err != nil {
		setupJobError = err.Error()
	}
	persistSetupProgressLocked()
}

func recordSetupProgress(status, step, detail string) {
	setupJobMu.Lock()
	defer setupJobMu.Unlock()
	setupJobLines = append(setupJobLines, SetupStepLine{
		Step:   step,
		Status: status,
		Detail: detail,
		At:     time.Now().Format("15:04:05"),
	})
	persistSetupProgressLocked()
}

func setupProgressReporterFromJob() setupProgressReporter {
	return setupProgressReporter(func(status, step, detail string) {
		recordSetupProgress(status, step, detail)
	})
}

func GetSetupJobProgress() SetupJobProgress {
	setupJobMu.Lock()
	progress := SetupJobProgress{
		Running:  setupJobRunning,
		Complete: setupJobDone,
		Error:    setupJobError,
		Message:  setupJobMessage,
		Lines:    append([]SetupStepLine(nil), setupJobLines...),
	}
	setupJobMu.Unlock()

	if loaded, ok := loadSetupProgressFile(); ok {
		if len(loaded.Lines) > len(progress.Lines) {
			progress.Lines = loaded.Lines
		}
		if loaded.Complete {
			progress.Complete = true
			progress.Message = loaded.Message
			progress.Running = false
		}
		if loaded.Error != "" {
			progress.Error = loaded.Error
		} else if loaded.Running && !loaded.Complete {
			progress.Running = true
		}
		if loaded.UpdatedAt != "" {
			progress.UpdatedAt = loaded.UpdatedAt
		}
	}

	progress.Configured = IsConfigured()
	if progress.Configured && progress.Complete {
		progress.Running = false
	}
	if progress.UpdatedAt == "" {
		progress.UpdatedAt = time.Now().Format("15:04:05")
	}
	return progress
}

func persistSetupProgressLocked() {
	_ = os.MkdirAll(filepath.Dir(setupProgressFile), 0755)
	payload := SetupJobProgress{
		Running:    setupJobRunning,
		Complete:   setupJobDone,
		Error:      setupJobError,
		Message:    setupJobMessage,
		Configured: IsConfigured(),
		UpdatedAt:  time.Now().Format(time.RFC3339),
		Lines:      append([]SetupStepLine(nil), setupJobLines...),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = os.WriteFile(setupProgressFile, data, 0644)
}

func loadSetupProgressFile() (SetupJobProgress, bool) {
	data, err := os.ReadFile(setupProgressFile)
	if err != nil {
		return SetupJobProgress{}, false
	}
	var progress SetupJobProgress
	if err := json.Unmarshal(data, &progress); err != nil {
		return SetupJobProgress{}, false
	}
	return progress, true
}

func tryStartSetupJob() bool {
	setupJobMu.Lock()
	defer setupJobMu.Unlock()
	if setupJobRunning {
		return false
	}
	setupJobRunning = true
	setupJobDone = false
	setupJobError = ""
	setupJobMessage = ""
	setupJobLines = nil
	persistSetupProgressLocked()
	return true
}

func isSetupJobRunning() bool {
	setupJobMu.Lock()
	defer setupJobMu.Unlock()
	return setupJobRunning
}

func startSetupJobAsync(cfg RouterConfig, authKey string) {
	go func() {
		progress := setupProgressReporterFromJob()
		err := ApplyBootstrapWithProgress(cfg, authKey, progress)
		if err != nil {
			finishSetupJob(err, "")
			return
		}

		ips := getManagementAccessIPs()
		msg := "Router configured. Open /login with your dashboard password."
		if len(ips) > 0 {
			msg = "Router configured. Login at http://" + ips[0] + ":5000/login"
		}
		finishSetupJob(nil, msg)
	}()
}
