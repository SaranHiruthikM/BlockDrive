package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/SaranHiruthik/BlockDrive/pkg/models"
)

func main() {
	serverAddr := flag.String("server", "http://localhost:8080", "Address of the node to contact")
	action := flag.String("action", "status", "Action: upload, download, status, ledger")
	filePath := flag.String("file", "", "Path to file for upload/download")
	fileID := flag.String("id", "", "File ID for download")
	flag.Parse()

	switch *action {
	case "upload":
		uploadFile(*serverAddr, *filePath)
	case "download":
		downloadFile(*serverAddr, *fileID, *filePath)
	case "status":
		getStatus(*serverAddr)
	case "ledger":
		getLedger(*serverAddr)
	default:
		fmt.Println("Unknown action")
	}
}

func downloadFile(addr, fileID, outputPath string) {
	if fileID == "" || outputPath == "" {
		fmt.Println("File ID and output path required for download")
		return
	}

	resp, err := http.Get(addr + "/download?id=" + fileID)
	if err != nil {
		fmt.Printf("Error requesting download: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Download failed with status: %s\n", resp.Status)
		return
	}

	out, err := os.Create(outputPath)
	if err != nil {
		fmt.Printf("Error creating output file: %v\n", err)
		return
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		fmt.Printf("Error saving file: %v\n", err)
		return
	}

	fmt.Printf("File downloaded successfully to %s\n", outputPath)
}

func uploadFile(addr, path string) {
	if path == "" {
		fmt.Println("File path required")
		return
	}

	file, err := os.Open(path)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		return
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return
	}
	io.Copy(part, file)
	writer.Close()

	req, _ := http.NewRequest("POST", addr+"/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error uploading: %v\n", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := ioutil.ReadAll(resp.Body)
	fmt.Printf("Upload response: %s\n", string(respBody))
}

func getStatus(addr string) {
	// Not implemented yet on server specific endpoint, maybe just check /ledger for now
	fmt.Println("Status check not fully implemented")
}

func getLedger(addr string) {
	resp, err := http.Get(addr + "/ledger")
	if err != nil {
		fmt.Printf("Error fetching ledger: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	var chain []models.Block
	json.Unmarshal(body, &chain)

	fmt.Println("Blockchain Ledger:")
	for _, block := range chain {
		fmt.Printf("Block %d [%s] - %d ops\n", block.Index, block.Hash, len(block.Operations))
		for _, op := range block.Operations {
			fmt.Printf("  Op: %s on %s\n", op.Type, time.Unix(op.Timestamp, 0))
		}
	}
}
