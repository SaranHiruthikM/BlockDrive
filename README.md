# BlockDrive - Distributed Storage System

A distributed file storage system implementing core distributed systems concepts in Go.

## Features implemented
- **Replication**: Files are split into chunks and replicated across nodes.
- **Leader Election**: Automated leader election using a Bully-like algorithm.
- **Fault Tolerance**: System continues to work if nodes fail (re-election).
- **Blockchain Audit Log**: All operations are recorded in a tamper-proof ledger.
- **Synchronization**: Nodes sync state via heartbeat and ledger requests.

## Architecture
- `cmd/node`: Main entry point for storage nodes.
- `cmd/client`: CLI client for uploading files and querying status.
- `internal/node`: Core node logic, HTTP handlers.
- `internal/election`: Leader election logic.
- `internal/blockchain`: Audit log implementation.
- `internal/storage`: Local file chunk storage.

## How to Run (Simulation on one machine)

1. **Build**
   ```bash
   go build -o node.exe ./cmd/node
   go build -o client.exe ./cmd/client
   ```

2. **Start Nodes** (OPEN 5 TERMINALS)

   Terminal 1 (Node 1):
   ```bash
   ./node.exe -id node1 -addr :8081 -peers localhost:8082,localhost:8083,localhost:8084,localhost:8085 -storage ./data/node1 -chain ./data/chain1.json
   ```

   Terminal 2 (Node 2):
   ```bash
   ./node.exe -id node2 -addr :8082 -peers localhost:8081,localhost:8083,localhost:8084,localhost:8085 -storage ./data/node2 -chain ./data/chain2.json
   ```

   Terminal 3 (Node 3):
   ```bash
   ./node.exe -id node3 -addr :8083 -peers localhost:8081,localhost:8082,localhost:8084,localhost:8085 -storage ./data/node3 -chain ./data/chain3.json
   ```

   ... and so on for 5 nodes.

3. **Client Operations**

   Upload a file:
   ```bash
   ./client.exe -action upload -file ./go.mod -server http://localhost:8081
   ```

   View Ledger:
   ```bash
   ./client.exe -action ledger -server http://localhost:8081
   ```
