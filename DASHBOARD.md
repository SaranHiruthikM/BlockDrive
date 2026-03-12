# BlockDrive - Distributed Storage System

## Dashboard

A web-based dashboard is available for monitoring the system.

### How to Run

1.  **Build**
    ```powershell
    go build -o dashboard.exe ./cmd/dashboard
    ```

2.  **Run**
    ```powershell
    ./dashboard.exe -port :8000 -nodes localhost:8081,localhost:8082,localhost:8083,localhost:8084,localhost:8085
    ```

3.  **Access**
    Open your browser to [http://localhost:8000](http://localhost:8000)

### Features
-   **Cluster Status**: View live status of all nodes (Leader/Follower, Blocks).
-   **File Upload**: Drag & drop interface to upload files to the cluster.
-   **Blockchain Ledger**: Visualize the tamper-proof audit log.
