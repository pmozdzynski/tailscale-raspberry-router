package handlers

import (
	"encoding/json"
	"log"
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
	Running     bool            `json:"running"`
	Complete    bool            `json:"complete"`
	Error       string          `json:"error,omitempty"`
	Message     string          `json:"message,omitempty"`
	Configured  bool            `json:"configured"`
	Interrupted bool            `json:"interrupted,omitempty"`
	UpdatedAt   string          `json:"updated_at"`
	Lines       []SetupStepLine `json:"lines"`
}

var (
	setupJobMu      sync.Mutex
	setupJobRunning bool
	setupJobLines   []SetupStepLine
	setupJobError   string
	setupJobMessage string
	setupJobDone    bool
)

func init() {
	reconcileStaleSetupJob()
}

// reconcileStaleSetupJob clears progress left from a prior process (e.g. after systemctl restart).
func reconcileStaleSetupJob() {
	setupJobMu.Lock()
	defer setupJobMu.Unlock()

	if setupJobRunning {
		return
	}

	loaded, ok := loadSetupProgressFileUnlocked()
	if !ok {
		return
	}

	if loaded.Complete {
		return
	}

	if loaded.Error != "" {
		return
	}

	if !loaded.Running {
		return
	}

	if IsConfigured() {
		loaded.Running = false
		loaded.Complete = true
		loaded.Message = "Router configured."
		writeSetupProgressFileUnlocked(loaded)
		return
	}

	log.Println("Setup: clearing abandoned install progress from a previous run")
	_ = os.Remove(setupProgressFile)
	setupJobLines = nil
	setupJobDone = false
	setupJobError = ""
	setupJobMessage = ""
}

func ClearSetupProgress() {
	setupJobMu.Lock()
	defer setupJobMu.Unlock()
	setupJobRunning = false
	setupJobDone = false
	setupJobError = ""
	setupJobMessage = ""
	setupJobLines = nil
	_ = os.Remove(setupProgressFile)
}

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
	reconcileStaleSetupJob()

	setupJobMu.Lock()
	progress := SetupJobProgress{
		Running:  setupJobRunning,
		Complete: setupJobDone,
		Error:    setupJobError,
		Message:  setupJobMessage,
		Lines:    append([]SetupStepLine(nil), setupJobLines...),
	}
	setupJobMu.Unlock()

	// Merge historical lines from disk when this process has not started a job yet.
	if !setupJobRunning && len(progress.Lines) == 0 {
		if loaded, ok := loadSetupProgressFileUnlocked(); ok {
			progress.Lines = loaded.Lines
			if loaded.Complete {
				progress.Complete = true
				progress.Message = loaded.Message
			}
			if loaded.Error != "" {
				progress.Error = loaded.Error
			}
			if loaded.UpdatedAt != "" {
				progress.UpdatedAt = loaded.UpdatedAt
			}
		}
	}

	progress.Configured = IsConfigured()
	if progress.Configured && !progress.Complete && !progress.Running && progress.Error == "" {
		progress.Complete = true
		progress.Message = "Router configured. Open /login."
	}
	if progress.Configured && progress.Complete {
		progress.Running = false
	}
	if progress.UpdatedAt == "" {
		progress.UpdatedAt = time.Now().Format(time.RFC3339)
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
	writeSetupProgressFileUnlocked(payload)
}

func writeSetupProgressFileUnlocked(payload SetupJobProgress) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = os.WriteFile(setupProgressFile, data, 0644)
}

func loadSetupProgressFileUnlocked() (SetupJobProgress, bool) {
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
	reconcileStaleSetupJob()

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
	_ = os.Remove(setupProgressFile)
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
