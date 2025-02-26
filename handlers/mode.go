package handlers

import (
	"encoding/json"
	"io/ioutil"
	"log"
)

const modeFile = "/etc/tailscale-mode.json" // Persistent storage for mode

// Struct for saving/restoring state
type ModeState struct {
	Mode string `json:"mode"`
}

var CurrentMode = LoadMode() // Load mode at startup

// Load mode from file
func LoadMode() string {
	data, err := ioutil.ReadFile(modeFile)
	if err != nil {
		log.Println("No previous mode found, defaulting to direct.")
		return "direct" // Default if no mode is saved
	}
	var state ModeState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Println("Error reading mode file, defaulting to direct.")
		return "direct"
	}
	return state.Mode
}

// Save mode to file
func SaveMode(mode string) {
	state := ModeState{Mode: mode}
	data, _ := json.Marshal(state)
	ioutil.WriteFile(modeFile, data, 0644)
}
