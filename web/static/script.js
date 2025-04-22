// Elements
const statusEl = document.getElementById("status");
const playersEl = document.getElementById("players");
const logbox = document.getElementById("logbox");
const startBtn = document.getElementById("start");
const stopBtn = document.getElementById("stop");

// Disable buttons initially
startBtn.disabled = true;
stopBtn.disabled = true;

// WebSocket connection
const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const ws = new WebSocket(`${wsProtocol}//${location.host}/ws`);

// WebSocket handlers
ws.onopen = () => {
    logbox.textContent = "Connected to server.";
};

ws.onmessage = (msg) => {
    try {
        const data = JSON.parse(msg.data);

        // Update status
        if (data.status === "running") {
            statusEl.textContent = "Running";
            statusEl.className = "status green";
            startBtn.disabled = true;
            stopBtn.disabled = false;
        } else {
            statusEl.textContent = "Stopped";
            statusEl.className = "status red";
            startBtn.disabled = false;
            stopBtn.disabled = true;
        }

        // Update players list
        playersEl.innerHTML = "";
        if (data.players && data.players.length > 0) {
            data.players.forEach(p => {
                const li = document.createElement("li");
                li.textContent = p;
                playersEl.appendChild(li);
            });
        } else {
            const li = document.createElement("li");
            li.textContent = "No players online";
            li.style.fontStyle = "italic";
            li.style.color = "#666";
            playersEl.appendChild(li);
        }

        // Update logs
        if (data.logs && data.logs.length > 0) {
            logbox.textContent = data.logs.join("\n");
            // Auto-scroll to bottom
            logbox.scrollTop = logbox.scrollHeight;
        }
    } catch (e) {
        console.error("Error parsing message:", e);
        logbox.textContent += "\nError receiving data from server.";
    }
};

ws.onclose = () => {
    statusEl.textContent = "Disconnected";
    statusEl.className = "status red";
    startBtn.disabled = true;
    stopBtn.disabled = true;
    logbox.textContent += "\nConnection closed. Reconnecting in 3 seconds...";

    setTimeout(() => location.reload(), 3000);
};

ws.onerror = (error) => {
    console.error("WebSocket error:", error);
    logbox.textContent += "\nWebSocket connection error. Please refresh the page.";
};

// Button handlers
startBtn.onclick = () => {
    startBtn.disabled = true;
    fetch("/start", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        }
    })
        .then(response => {
            if (!response.ok) {
                return response.text().then(text => {
                    throw new Error(text || "Failed to start server");
                });
            }
            return response.text();
        })
        .catch(error => {
            console.error("Error starting server:", error);
            logbox.textContent += "\nError starting server: " + error.message;
            startBtn.disabled = false;
        });
};

stopBtn.onclick = () => {
    stopBtn.disabled = true;
    fetch("/stop", {
        method: "POST",
        headers: {
            "Content-Type": "application/json"
        }
    })
        .then(response => {
            if (!response.ok) {
                return response.text().then(text => {
                    throw new Error(text || "Failed to stop server");
                });
            }
            return response.text();
        })
        .catch(error => {
            console.error("Error stopping server:", error);
            logbox.textContent += "\nError stopping server: " + error.message;
            stopBtn.disabled = false;
        });
};
