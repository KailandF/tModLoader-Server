package main

import (
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// Global variables to track the running server process and its stdin
var serverCmd *exec.Cmd
var serverIn io.WriteCloser

func main() {
	// Set up HTTP handlers for the web interface
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/start", startHandler)
	http.HandleFunc("/stop", stopHandler)

	// Start the web server on port 8080
	log.Println("Web dashboard running at :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

// indexHandler serves the HTML dashboard web page
func indexHandler(w http.ResponseWriter, _ *http.Request) {
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, "Failed to load page", http.StatusInternalServerError)
		log.Println("Template error:", err)
		return
	}
	err = tmpl.Execute(w, nil)
	if err != nil {
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		log.Println("Template execution error:", err)
	}
}

// startHandler launches the tModLoader server and sends required inputs
func startHandler(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	// Prevent multiple servers from starting
	if serverCmd != nil {
		http.Error(w, "Server already running", http.StatusConflict)
		return
	}

	// Command to start the server script
	cmd := exec.Command("/home/pi/tModLoader/start-tModLoaderServer.sh")

	// Get stdin pipe to send automated input
	stdin, err := cmd.StdinPipe()
	if err != nil {
		http.Error(w, "Failed to get stdin", http.StatusInternalServerError)
		log.Println("stdin error:", err)
		return
	}

	// Connect server output to this process's stdout/stderr for logging
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Start the server process
	if err := cmd.Start(); err != nil {
		http.Error(w, "Failed to start server", http.StatusInternalServerError)
		log.Println("Start error:", err)
		return
	}

	// Save command and stdin for later use
	serverCmd = cmd
	serverIn = stdin

	// Send the required sequence of inputs to the server
	go func() {
		time.Sleep(2 * time.Second) // Give time for server to be ready for input

		// Inputs expected by the tModLoader server script
		inputs := []string{"y\n", "f\n", "3\n", "1\n", "\n", "n\n", "\n"}
		for _, input := range inputs {
			_, err := io.WriteString(stdin, input)
			if err != nil {
				log.Println("Error writing to stdin:", err)
				return
			}
			time.Sleep(500 * time.Millisecond) // Small delay between inputs
		}

		// Close stdin once all inputs are sent
		if err := stdin.Close(); err != nil {
			log.Println("Error closing stdin:", err)
		}
	}()

	log.Println("tModLoader server start initiated.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// stopHandler sends the "exit" command to stop the server cleanly
func stopHandler(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	// Ensure the server is running before trying to stop it
	if serverCmd == nil || serverIn == nil {
		http.Error(w, "Server not running", http.StatusBadRequest)
		return
	}

	// Send "exit" to the server to stop it gracefully
	_, err := io.WriteString(serverIn, "exit\n")
	if err != nil {
		http.Error(w, "Failed to send stop command", http.StatusInternalServerError)
		log.Println("Error writing 'exit' to stdin:", err)
		return
	}

	// Wait for the server process to finish in a goroutine
	go func() {
		err := serverCmd.Wait()
		if err != nil {
			log.Println("Server process wait error:", err)
		}
		// Reset state
		serverCmd = nil
		serverIn = nil
		log.Println("Server stopped.")
	}()

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
