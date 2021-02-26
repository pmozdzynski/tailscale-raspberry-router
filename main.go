package main

import (
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"

	"tailscale-raspberry-router/handlers"
)

// Ensure the program is run as root
func checkRootPrivileges() {
	if os.Geteuid() != 0 {
		log.Fatal("This program must be run as root. Try using: sudo ./tailscale-raspberry-router")
	}
}

// Ensure Tailscale is installed and running
func checkTailscaleStatus() {
	_, err := exec.LookPath("tailscale")
	if err != nil {
		log.Fatal("Tailscale is not installed. Please install it using: sudo apt install tailscale")
	}

	cmd := exec.Command("tailscale", "status")
	err = cmd.Run()
	if err != nil {
		log.Fatal("Tailscale is not running. Start it using: sudo systemctl start tailscaled && sudo tailscale up")
	}

	log.Println("Tailscale is installed and running.")
}

func main() {
	checkRootPrivileges()  // Ensure script is running as root
	checkTailscaleStatus() // Ensure Tailscale is installed and running

	// Serve all files in ./templates
	fs := http.FileServer(http.Dir("./templates"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Serve index.html when accessing "/"
		if r.URL.Path == "/" {
			http.ServeFile(w, r, "./templates/index.html")
			return
		}
		// Serve other files normally
		fs.ServeHTTP(w, r)
	})

	// API Endpoints
	http.HandleFunc("/status", handlers.StatusHandler)
	http.HandleFunc("/set-mode", handlers.SetModeHandler)

	// Debugging: Log available files in the templates directory
	files, err := os.ReadDir("./templates")
	if err != nil {
		log.Fatalf("Error reading templates directory: %v", err)
	}
	for _, file := range files {
		log.Println("Found file:", file.Name())
	}

	// Start the server in a separate goroutine
	go func() {
		log.Println("Starting server on :5000")
		err := http.ListenAndServe(":5000", nil)
		if err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for the server to be fully up
	time.Sleep(3 * time.Second)

	// Restore the previous mode in a separate goroutine
	go handlers.RestorePreviousMode()

	// Prevent main() from exiting
	select {} // Block forever (server runs in a goroutine)
}
