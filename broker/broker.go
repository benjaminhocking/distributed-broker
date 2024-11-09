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
        fmt.Printf("Request: %v\n", request)
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

func calculateAliveCells(world [][]uint8) int {
    aliveCount := 0
    for y := range world {
        for x := range world[y] {
            if world[y][x] == 255 {
                aliveCount++
            }
        }
    }
    return aliveCount
}

func (s *SecretStringOperations) Start(req stubs.BrokerRequest, res *stubs.Response) (err error) {
    regions := splitBoard(req.ImageHeight, req.ImageWidth, req.Workers)
    fmt.Printf("Regions: %v\n", regions)
    workers := buildWorkers(regions)

    world := req.World
    var worldSlices []WorldSlice
    currentTurn := 0
    
    for t := 0; t < req.Turns; t++ {
        select {
        case responseChan := <-s.aliveCellsChannel:
            // Count alive cells in current world state
            aliveCount := calculateAliveCells(world)
            
            responseChan <- GameState{
                AliveCells: aliveCount,
                CurrentTurn: currentTurn,
            }
            
        default:
            worldSlices = []WorldSlice{}
            resultChan := make(chan WorldSlice, len(workers))

            for _, worker := range workers {
                // Launch a goroutine for each worker
                go func(w WorkerConfig) {
                    worldSlice := workerNextState(w, world, req.ImageWidth, req.ImageHeight)
                    resultChan <- WorldSlice{World: worldSlice, Region: w.Region}
                }(worker)
            }

            // Collect the results from all goroutines
            for i := 0; i < len(workers); i++ {
                ws := <-resultChan
                worldSlices = append(worldSlices, ws)
            }
            
            world = mergeWorldSlices(worldSlices)
            currentTurn++
        }
    }

    return nil
}

func (s *SecretStringOperations) AliveCellsCount(req stubs.AliveCellsCountRequest, res *stubs.AliveCellsCountResponse) (err error) {
    responseChannel := make(chan GameState)
    s.aliveCellsChannel <- responseChannel
    state := <-responseChannel
    res.CellsAlive = state.AliveCells
    res.Turns = state.CurrentTurn
    return nil
}

func mergeWorldSlices(worldSlices []WorldSlice) [][]uint8 {
    if len(worldSlices) == 0 {
        return nil
    }

    // Get dimensions from the first slice's region
    firstRegion := worldSlices[0].Region
    totalHeight := 0
    width := firstRegion.X2 - firstRegion.X1 + 1

    // Calculate total height by finding max Y2
    for _, slice := range worldSlices {
        if slice.Region.Y2 > totalHeight {
            totalHeight = slice.Region.Y2 + 1
        }
    }

    // Initialize merged world
    mergedWorld := make([][]uint8, totalHeight)
    for i := range mergedWorld {
        mergedWorld[i] = make([]uint8, width)
    }

    // Copy each slice's data into the correct position
    for _, slice := range worldSlices {
        region := slice.Region
        for y := region.Y1; y <= region.Y2; y++ {
            copy(mergedWorld[y][region.X1:region.X2+1], slice.World[y][region.X1:region.X2+1])
        }
    }

    return mergedWorld
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
