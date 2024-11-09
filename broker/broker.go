package main

import (
	"flag"
    "math/rand"
    "time"
    "strings"
	"fmt"
	"net"
	"net/rpc"
	"uk.ac.bris.cs/gameoflife/stubs"
)

var (
	listener net.Listener
	shutdown = make(chan bool)
    instances = []string{"35.173.134.238", "3.89.9.195", "34.238.50.127", "98.84.152.24"}
)


func dialWorker(workerConfig WorkerConfig) (*rpc.Client, error){
    var err error
    addr := fmt.Sprintf("%s:8030", workerConfig.IpAddr)
    fmt.Printf("connect to %s\n", addr)
    rpcClient, err := rpc.Dial("tcp", addr)

    if err != nil {
        return nil, err
    }
    return rpcClient, nil
}

type SecretStringOperations struct {
	aliveCellsChannel chan chan GameState
	worldStateChannel chan chan WorldState
	stopChannel chan bool
	pauseChannel chan bool
	isPaused bool
}

func NewSecretStringOperations() *SecretStringOperations {
    return &SecretStringOperations{
        aliveCellsChannel: make(chan chan GameState),
        worldStateChannel: make(chan chan WorldState),
        stopChannel:       make(chan bool),
        pauseChannel:      make(chan bool),
    }
}

type WorkerConfig struct{
    Region stubs.CoordinatePair
    IpAddr string
}

type GameState struct {
    AliveCells int
    CurrentTurn int
}

type WorldState struct {
    World       [][]uint8
    CurrentTurn int
}

type WorldSlice struct{
    World       [][]uint8
    Region      stubs.CoordinatePair
}

func workerNextState(workerConfig WorkerConfig, world [][]uint8, imageWidth, imageHeight int) [][]uint8{
    fmt.Println("WorkerNextState")
    fmt.Printf("WorkerConfig: %v\n", workerConfig)
    fmt.Printf("Region: %v\n", workerConfig.Region)
    client, err := dialWorker(workerConfig)
    if err == nil{
        request := stubs.WorkerRequest{
            World:world, 
            Region: workerConfig.Region,
            ImageWidth: imageWidth,
            ImageHeight: imageHeight,
        }
        response := new(stubs.Response)
        client.Call("SecretStringOperations.NextState", request, response)
        return response.UpdatedWorld
    }else{
        fmt.Printf("Error dialing worker: %v\n", err)
    }
    return nil
}

func buildWorkers(regions []stubs.CoordinatePair) []WorkerConfig {
    workers := []WorkerConfig{}
    var worker WorkerConfig
    for i, region := range regions{
        worker = WorkerConfig{
            Region: region,
            IpAddr: instances[i],
        }
        workers = append(workers, worker)
    }
    return workers
}

func (s *SecretStringOperations) Start(req stubs.BrokerRequest, res *stubs.Response) (err error) {
	//world := req.World
    regions := splitBoard(req.ImageHeight, req.ImageWidth, req.Workers)
    fmt.Printf("Regions: %v\n", regions)
    workers := buildWorkers(regions)

    //var world [][]uint8
    var worldSlices []WorldSlice
    for t := 0; t < req.Turns; t++ {
        worldSlices = []WorldSlice{}
        resultChan := make(chan WorldSlice, len(workers))

        for _, worker := range workers {
            // Launch a goroutine for each worker
            go func(w WorkerConfig) {
                worldSlice := workerNextState(w, req.World, req.ImageWidth, req.ImageHeight)
                resultChan <- WorldSlice{World: worldSlice, Region: w.Region}
            }(worker)
        }

        // Collect the results from all goroutines
        for i := 0; i < len(workers); i++ {
            ws := <-resultChan
            worldSlices = append(worldSlices, ws)
        }
        //collect all of these together
        //world = mergeWorldSlices(worldSlices)
    }

	return nil
}

func splitBoard(H, W, workers int) []stubs.CoordinatePair {
    if workers <= 0 {
        return nil // Return nil if workers is zero or negative
    }
    
    var regions []stubs.CoordinatePair
    rowsPerWorker := H / workers     // Base number of rows each worker gets
    extraRows := H % workers         // Rows that need to be distributed

    startRow := 0
    for i := 0; i < workers; i++ {
        // Calculate the end row for this worker's region
        endRow := startRow + rowsPerWorker
        if i < extraRows { // Distribute the extra rows among the first 'extraRows' workers
            endRow++
        }

        // Append the region for this worker
        regions = append(regions, stubs.CoordinatePair{
            X1: 0, Y1: startRow,
            X2: W - 1, Y2: endRow - 1,
        })

        // Update startRow for the next worker's region
        startRow = endRow
    }

    return regions
}


func main() {
	pAddr := flag.String("port", "8030", "Port to listen on")
    flag.Parse()
    rand.Seed(time.Now().UnixNano())
    rpc.Register(NewSecretStringOperations())
    
    var err error
    listener, err = net.Listen("tcp", "localhost:8030")
    if err != nil {
        fmt.Printf("Error starting server: %v\n", err)
        return
    }
    defer listener.Close()
    
    fmt.Printf("Server is listening on port %s...\n", *pAddr)
    
    // Create a separate goroutine for accepting connections
    go func() {
        for {
            select {
            case <-shutdown:
                return
            default:
                conn, err := listener.Accept()
                if err != nil {
                    if !strings.Contains(err.Error(), "use of closed network connection") {
                        fmt.Printf("Accept error: %v\n", err)
                    }
                    return
                }
                go rpc.ServeConn(conn)
            }
        }
    }()

    // Wait for shutdown signal
    <-shutdown
    fmt.Println("Server shutdown complete")
}
