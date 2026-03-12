package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

//go:embed templates/index.html
var templateFS embed.FS

var (
	nodesAddr []string
	templates *template.Template
)

func main() {
	port := flag.String("port", ":8000", "Dashboard port")
	nodesList := flag.String("nodes", "localhost:8081,localhost:8082,localhost:8083,localhost:8084,localhost:8085", "List of node addresses")
	flag.Parse()

	nodesAddr = strings.Split(*nodesList, ",")
	var err error

	// Parse template from embedded filesystem
	templates, err = template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		log.Fatalf("Error parsing embedded templates: %v", err)
	}

	http.HandleFunc("/", handleDashboard)
	http.HandleFunc("/upload", handleProxyUpload)

	fmt.Printf("Dashboard running at http://localhost%s\n", *port)
	if err := http.ListenAndServe(*port, nil); err != nil {
		log.Fatal(err)
	}
}

type NodeStatus struct {
	Address       string
	ID            string `json:"id"`
	State         string `json:"state"`
	LeaderID      string `json:"leader_id"`
	BlockHeight   int    `json:"block_height"`
	LastHeartbeat int64  `json:"last_heartbeat"`
	IsAlive       bool
	Error         string
}

type DashboardData struct {
	Nodes     []NodeStatus
	Ledger    interface{}
	Timestamp string
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	data := DashboardData{
		Timestamp: time.Now().Format(time.RFC1123),
		Nodes:     make([]NodeStatus, 0),
	}

	// Fetch status from all nodes
	nodeChan := make(chan NodeStatus)
	for _, addr := range nodesAddr {
		go func(a string) {
			nodeChan <- fetchNodeStatus(a)
		}(addr)
	}

	for range nodesAddr {
		status := <-nodeChan
		data.Nodes = append(data.Nodes, status)
	}

	// Fetch ledger from the first available node
	for _, node := range data.Nodes {
		if node.IsAlive {
			ledger := fetchLedger(node.Address)
			if ledger != nil {
				data.Ledger = ledger
				break
			}
		}
	}

	// If templates fail, fallback to simple text
	err := templates.Execute(w, data)
	if err != nil {
		log.Printf("Template execution error: %v", err)
		http.Error(w, "Template Error: "+err.Error(), http.StatusInternalServerError)
	}
}

func fetchNodeStatus(addr string) NodeStatus {
	client := http.Client{Timeout: 500 * time.Millisecond}
	status := NodeStatus{Address: addr, IsAlive: false}

	resp, err := client.Get("http://" + addr + "/status")
	if err != nil {
		status.Error = "Unreachable"
		return status
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		status.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return status
	}

	var rawStatus struct {
		ID            string `json:"id"`
		State         string `json:"state"`
		LeaderID      string `json:"leader_id"`
		BlockHeight   int    `json:"block_height"`
		LastHeartbeat int64  `json:"last_heartbeat"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rawStatus); err == nil {
		status.ID = rawStatus.ID
		status.State = rawStatus.State
		status.LeaderID = rawStatus.LeaderID
		status.BlockHeight = rawStatus.BlockHeight
		status.LastHeartbeat = rawStatus.LastHeartbeat
		status.IsAlive = true
	} else {
		status.Error = "Wait JSON"
	}

	return status
}

func fetchLedger(addr string) interface{} {
	client := http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://" + addr + "/ledger")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var ledger interface{}
	json.NewDecoder(resp.Body).Decode(&ledger)
	return ledger
}

func handleProxyUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// 1. Get file from form
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Bad Request: Missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 2. Determine target node (LEADER preferably)
	targetAddr := nodesAddr[0] // Default to first
	// Try to find a leader from cache or quick scan?
	// For now, just pick first alive one or random.

	// 3. Prepare multipart forward
	// Streaming logic needs pipe
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		defer writer.Close()
		part, err := writer.CreateFormFile("file", header.Filename)
		if err != nil {
			return
		}
		io.Copy(part, file)
	}()

	// 4. Send request
	resp, err := http.Post("http://"+targetAddr+"/upload", writer.FormDataContentType(), pr)
	if err != nil {
		http.Error(w, "Backend Node Failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 5. Show result
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, "<h1>Upload Result</h1><pre>%s</pre><a href='/'>Back</a>", string(bodyBytes))
}
