package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Configuration constants
const (
	screenName     = "tmod_session"                             // Name of the screen session for the Terraria server
	expectScript   = "$HOME/Desktop/scripts/tmod_server.expect" // Path to the expect script that starts the server
	hardcopyOutput = "/tmp/tmod_screen_out.txt"                 // Path to save screen output
)

// Global variables
var (
	logs      []string                         // Stores server logs
	logMutex  sync.Mutex                       // Mutex to protect concurrent access to logs
	upgrader  = websocket.Upgrader{}           // WebSocket upgrader for HTTP to WebSocket protocol
	clients   = make(map[*websocket.Conn]bool) // Map of active WebSocket connections
	clientsMu sync.Mutex                       // Mutex to protect concurrent access to clients map
)

// WSMessage defines the structure for WebSocket messages
type WSMessage struct {
	Status  string   `json:"status"`         // Server status (running/stopped)
	Players []string `json:"players"`        // List of currently connected players
	Logs    []string `json:"logs,omitempty"` // Recent server logs
}

// main initializes the server and sets up HTTP routes
func main() {
	// Start a goroutine to periodically check server status
	go monitorServerStatus()

	// Set up HTTP routes without authentication middleware
	http.HandleFunc("/", indexHandler)      // Serve index page
	http.HandleFunc("/ws", wsHandler)       // WebSocket endpoint
	http.HandleFunc("/start", startHandler) // Start server endpoint
	http.HandleFunc("/stop", stopHandler)   // Stop server endpoint
	// Serve static files from web directory
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static"))))

	log.Println("Server started at http://kwf-go.online")
	// Define the HTTP server
	srv := &http.Server{Addr: ":8080", Handler: nil}

	// Start the server in a goroutine
	go func() {
		log.Println("Server started at http://kwf-go.online")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Listen error: %s\n", err)
		}
	}()

	// Wait for system interrupt signal (e.g. Ctrl+C)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Shutdown with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited cleanly.")

}

// indexHandler serves the main HTML page
func indexHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Received request for index page") // Log when this handler is called
	http.ServeFile(w, r, "./web/index.html")
}

// startHandler handles requests to start the Terraria server
func startHandler(w http.ResponseWriter, r *http.Request) {
	// Ensure endpoint only accepts POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if server is already running
	if isServerRunning() {
		http.Error(w, "Server already running", http.StatusConflict)
		return
	}

	// Start the Terraria server in a new screen session
	cmd := exec.Command("screen", "-S", screenName, "-dm", "bash", "-c", expectScript)
	if err := cmd.Run(); err != nil {
		http.Error(w, "Failed to start server", http.StatusInternalServerError)
		addLog("Failed to start server: " + err.Error())
		return
	}

	// Log successful start and broadcast status to clients
	addLog("Server started at " + time.Now().Format(time.RFC1123))
	broadcastStatus()
}

// stopHandler handles requests to stop the Terraria server
func stopHandler(w http.ResponseWriter, r *http.Request) {
	// Ensure endpoint only accepts POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if server is running
	if !isServerRunning() {
		http.Error(w, "Server not running", http.StatusConflict)
		return
	}

	// Send exit command to the server
	sendScreenCommand("exit")

	// Log successful stop and broadcast status to clients
	addLog("Server stopped at " + time.Now().Format(time.RFC1123))
	broadcastStatus()
}

// isServerRunning checks if the server screen session exists
func isServerRunning() bool {
	cmd := exec.Command("screen", "-list")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	// Check if the screen session name is found in the output
	return strings.Contains(string(out), screenName)
}

// sendScreenCommand sends a command to the screen session
func sendScreenCommand(command string) {
	err := exec.Command("screen", "-S", screenName, "-X", "stuff", command+"\n").Run()
	if err != nil {
		fmt.Println("Could not send screen command")
		return
	}
}

// broadcastStatus sends current server status to all connected WebSocket clients
func broadcastStatus() {
	// Lock to prevent concurrent map access
	clientsMu.Lock()
	defer clientsMu.Unlock()

	// Prepare message with default "stopped" status
	msg := WSMessage{
		Status:  "stopped",
		Players: []string{},
		Logs:    getLastLogs(),
	}

	// Update message if server is running
	if isServerRunning() {
		msg.Status = "running"
		msg.Players = getCurrentPlayers()
	}

	// Marshal message to JSON
	data, err := json.Marshal(msg)
	if err != nil {
		log.Println("Error marshaling WebSocket message:", err)
		return
	}

	// Send message to all clients
	for client := range clients {
		if client == nil {
			continue
		}
		if err := client.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("Error sending message to client: %v", err)

			// Remove and close the faulty client connection
			delete(clients, client)
			if cerr := client.Close(); cerr != nil {
				log.Printf("Error closing WebSocket client: %v", cerr)
			}
		}
	}
}

// getLastLogs returns the most recent log entries (up to 10)
func getLastLogs() []string {
	logMutex.Lock()
	defer logMutex.Unlock()

	// Return all logs if fewer than 10
	if len(logs) <= 10 {
		return logs
	}
	// Otherwise return the last 10 logs
	return logs[len(logs)-10:]
}

// addLog adds a new log entry with timestamp
func addLog(entry string) {
	logMutex.Lock()
	defer logMutex.Unlock()

	// Format log entry with timestamp
	logs = append(logs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), entry))
}

// wsHandler handles WebSocket connections
func wsHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WS upgrade failed:", err)
		return
	}

	// Ensure connection is closed when handler exits
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("Error closing WebSocket connection: %v", err)
		}
	}()

	// Add client to active clients map
	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	// Keep connection open and handle incoming messages
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break // Exit loop on read error (client disconnect)
		}
	}

	// Remove client from map when disconnected
	clientsMu.Lock()
	delete(clients, conn)
	clientsMu.Unlock()
}

// monitorServerStatus periodically checks server status and updates clients
func monitorServerStatus() {
	for {
		// Save screen output if server is running
		if isServerRunning() {
			saveScreenOutput()
		}

		// Broadcast current status to all clients
		broadcastStatus()

		// Wait before next check
		time.Sleep(5 * time.Second)
	}
}

// saveScreenOutput saves the current screen content to a file
func saveScreenOutput() {
	err := exec.Command("screen", "-S", screenName, "-X", "hardcopy", hardcopyOutput).Run()
	if err != nil {
		fmt.Println("Could not save screen")
	}
}

// getCurrentPlayers retrieves the list of currently connected players
func getCurrentPlayers() []string {
	// Send the 'players' command to the server
	sendScreenCommand("players")

	// Wait for command to execute
	time.Sleep(2 * time.Second)

	// Save output to file
	saveScreenOutput()

	// Open the output file
	file, err := os.Open(hardcopyOutput)
	if err != nil {
		return []string{}
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Error closing file: %v", err)
		}
	}()

	// Read file line by line
	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Search for player list from bottom up (most recent first)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "Current players") || strings.HasPrefix(lines[i], "Players:") {
			return parsePlayersLine(lines[i])
		}
	}
	return []string{}
}

// parsePlayersLine extracts player names from a line of output
func parsePlayersLine(line string) []string {
	if strings.Contains(line, ":") {
		// Split line at first colon
		parts := strings.SplitN(line, ":", 2)
		if len(parts) > 1 {
			raw := strings.TrimSpace(parts[1])
			// Return empty list if no players
			if raw == "None" || raw == "" {
				return []string{}
			}
			// Split comma-separated list of players
			return strings.Split(raw, ", ")
		}
	}
	return []string{}
}
