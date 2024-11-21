package main

// Package main implements a broker server for the Game of Life distributed system.
// It manages worker nodes, handles client requests, and coordinates game state.

import (
	"flag"
    "math/rand"
    "time"
    "strings"
	"fmt"
	"net"
	"net/rpc"
	"uk.ac.bris.cs/gameoflife/stubs"
    "sync"
    "os"
)

// Global variables for server operation
var (
	listener net.Listener
	shutdown = make(chan bool)
    // IP addresses of worker nodes
    instances = []string{"44.212.56.54", "3.80.209.156", "18.212.12.114", "3.83.39.17", "184.72.211.183"} // Replace these IP addresses with those of your running ec2 instances
)

// dialWorker establishes RPC connection with a worker node
func dialWorker(ipAddr string) (*rpc.Client, error){
    var err error
    addr := fmt.Sprintf("%s:8030", ipAddr)
    fmt.Printf("connect to %s\n", addr)
    rpcClient, err := rpc.Dial("tcp", addr)
    
    if err != nil {
        return nil, err
    }
    return rpcClient, nil
}

// SecretStringOperations handles RPC methods and maintains game state
type SecretStringOperations struct {
	aliveCellsChannel chan chan GameState
	worldStateChannel chan chan WorldState
	stopChannel chan bool
	pauseChannel chan bool
	isPaused bool
}

// NewSecretStringOperations creates and initializes a new SecretStringOperations instance
func NewSecretStringOperations() *SecretStringOperations {
    return &SecretStringOperations{
        aliveCellsChannel: make(chan chan GameState),
        worldStateChannel: make(chan chan WorldState),
        stopChannel:       make(chan bool),
        pauseChannel:      make(chan bool),
    }
}

// WorkerConfig holds configuration and connection details for a worker node
type WorkerConfig struct{
    Region stubs.CoordinatePair
    IpAddr string
    Client *rpc.Client
}

// GameState represents the current state of the game
type GameState struct {
    AliveCells int
    CurrentTurn int
}

// WorldState represents the complete state of the game world
type WorldState struct {
    World       [][]uint8
    CurrentTurn int
}

// WorldSlice represents a portion of the game world assigned to a worker
type WorldSlice struct{
    World       [][]uint8
    Region      stubs.CoordinatePair
}

// getWorldRegion extracts a portion of the world for a worker, including halo cells
func getWorldRegion(world [][]uint8, region stubs.CoordinatePair) [][]uint8{
    height := region.Y2 - region.Y1 + 3  // Add halo rows
    width := region.X2 - region.X1 + 3   // Add halo columns
    
    regionSlice := make([][]uint8, height)
    for i := range regionSlice {
        regionSlice[i] = make([]uint8, width)
    }

    worldHeight := len(world)
    worldWidth := len(world[0])

    // Copy region data with wrapping at world boundaries
    for y := 0; y < height; y++ {
        for x := 0; x < width; x++ {
            worldY := ((region.Y1 - 1 + y) + worldHeight) % worldHeight
            worldX := ((region.X1 - 1 + x) + worldWidth) % worldWidth
            regionSlice[y][x] = world[worldY][worldX]
        }
    }

    return regionSlice
}

// workerNextState sends the current state to a worker and gets the next state
func workerNextState(workerConfig WorkerConfig, world [][]uint8, imageWidth, imageHeight int) [][]uint8{    
    if workerConfig.Client != nil{
        request := stubs.WorkerRequest{
            World:getWorldRegion(world, workerConfig.Region), 
            Region: workerConfig.Region,
        }
        response := new(stubs.Response)
        workerConfig.Client.Call("SecretStringOperations.NextState", request, response)
        return response.UpdatedWorld
    }else{
        fmt.Printf("Error dialing worker: %v\n", workerConfig.Client)
    }
    return nil
}

// buildWorkers initializes connections to all worker nodes
func buildWorkers(regions []stubs.CoordinatePair) []WorkerConfig {
    workers := []WorkerConfig{}
    var worker WorkerConfig
    for i, region := range regions{
        ipAddr := instances[i]
        client, err := dialWorker(ipAddr)
        if err == nil{
            worker = WorkerConfig{
                Region: region,
                IpAddr: ipAddr,
                Client: client,
            }
            workers = append(workers, worker)
        }
    }
    return workers
}

// calculateAliveCells counts the number of alive cells in the world
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

// mergeWorldSlices combines results from all workers into a single world state
func mergeWorldSlices(worldSlices []WorldSlice, world [][]uint8) [][]uint8 {
    if len(worldSlices) == 0 {
        return nil
    }

    totalHeight := 0
    width := worldSlices[0].Region.X2 - worldSlices[0].Region.X1 + 1
    for _, slice := range worldSlices {
        if slice.Region.Y2+1 > totalHeight {
            totalHeight = slice.Region.Y2 + 1
        }
    }

    mergedWorld := make([][]uint8, totalHeight)
    for i := range mergedWorld {
        mergedWorld[i] = make([]uint8, width)
        copy(mergedWorld[i], world[i])
    }

    var wg sync.WaitGroup

    for _, slice := range worldSlices {
        wg.Add(1)
        go func(ws WorldSlice) {
            defer wg.Done()
            region := ws.Region
            startY := region.Y1
            startX := region.X1

            for y := 0; y <= len(ws.World)-1; y++ {
                for x := 0; x <= len(ws.World[0])-1; x++ {
                    mergedWorld[startY+y][startX+x] = ws.World[y][x]
                }
            }
        }(slice)
    }

    wg.Wait()
    return mergedWorld
}

// splitBoard divides the game world into regions for parallel processing
func splitBoard(H, W, workers int) []stubs.CoordinatePair {
    if workers <= 0 {
        return nil
    }
    
    var regions []stubs.CoordinatePair
    rowsPerWorker := H / workers
    extraRows := H % workers

    startRow := 0
    for i := 0; i < workers; i++ {
        endRow := startRow + rowsPerWorker
        if i < extraRows {
            endRow++
        }

        regions = append(regions, stubs.CoordinatePair{
            X1: 0, Y1: startRow,
            X2: W - 1, Y2: endRow - 1,
        })

        startRow = endRow
    }

    return regions
}

// Start begins the game simulation and manages the main game loop
func (s *SecretStringOperations) Start(req stubs.BrokerRequest, res *stubs.Response) (err error) {
    regions := splitBoard(req.ImageHeight, req.ImageWidth, req.Workers)
    workers := buildWorkers(regions)

    s.isPaused = false

    world := req.World
    var worldSlices []WorldSlice
    currentTurn := 0
    
    for {
        select {
            case responseChan := <-s.aliveCellsChannel:
                aliveCount := calculateAliveCells(world)
                
                responseChan <- GameState{
                    AliveCells: aliveCount,
                    CurrentTurn: currentTurn,
                }
            
            case responseChan := <-s.worldStateChannel:
				state := WorldState{
					World: world,
					CurrentTurn: currentTurn,
				}
				responseChan <- state
            
            case <-s.stopChannel:
                res.UpdatedWorld = world
                res.Turns = currentTurn
				return nil
            default:
                if currentTurn >= req.Turns{
                    res.UpdatedWorld = world
                    res.Turns = currentTurn
                    return nil
                }
                
                if !s.isPaused{
                    fmt.Println("currentTurn")
                    fmt.Println(currentTurn)
                    worldSlices = []WorldSlice{}
                    resultChan := make(chan WorldSlice, len(workers))

                    for _, worker := range workers {
                        go func(w WorkerConfig) {
                            worldSlice := workerNextState(w, world, req.ImageWidth, req.ImageHeight)
                            resultChan <- WorldSlice{World: worldSlice, Region: w.Region}
                        }(worker)
                    }

                    for i := 0; i < len(workers); i++ {
                        ws := <-resultChan
                        worldSlices = append(worldSlices, ws)
                    }
                    
                    world = mergeWorldSlices(worldSlices, world)
                    currentTurn++
                }
        }
    }

    res.UpdatedWorld = world
    res.Turns = currentTurn

    return nil
}

// AliveCellsCount returns the current count of alive cells
func (s *SecretStringOperations) AliveCellsCount(req stubs.AliveCellsCountRequest, res *stubs.AliveCellsCountResponse) (err error) {
    responseChannel := make(chan GameState)
    s.aliveCellsChannel <- responseChannel
    state := <-responseChannel
    res.CellsAlive = state.AliveCells
    res.Turns = state.CurrentTurn
    return nil
}

// Save captures the current game state
func (s *SecretStringOperations) Save(req stubs.StateRequest, res *stubs.StateResponse) (err error) {
    worldStateChannel := make(chan WorldState)
    s.worldStateChannel <- worldStateChannel
    worldState := <-worldStateChannel
    res.World = worldState.World
    res.Turns = worldState.CurrentTurn
    res.Message = "Continuing"
    return nil
}

// Quit gracefully stops the game and returns final state
func (s *SecretStringOperations) Quit(req stubs.StateRequest, res *stubs.StateResponse) (err error) {
    worldStateChannel := make(chan WorldState)
    s.worldStateChannel <- worldStateChannel
    worldState := <-worldStateChannel
    res.World = worldState.World
    res.Turns = worldState.CurrentTurn
    res.Message = "Continuing"
    s.stopChannel <- true
    return nil
}

// Kill terminates the server
func (s *SecretStringOperations) Kill(req stubs.StateRequest, res *stubs.StateResponse) (err error) {
    fmt.Println("Shutting down server...")
    if listener != nil {
        listener.Close()
    }

    s.stopChannel <- true
    defer os.Exit(0)
    return nil
}

// Pause toggles the pause state of the game
func (s *SecretStringOperations) Pause(req stubs.StateRequest, res *stubs.StateResponse) (err error) {
    if(s.worldStateChannel == nil){
        fmt.Println("Game has not started, but pause command received")
    }

    var respWorld [][]uint8
    var respTurns int
    
    if(!s.isPaused){
        worldStateChannel := make(chan WorldState)
        s.worldStateChannel <- worldStateChannel
        worldState := <-worldStateChannel
        respWorld = worldState.World
        respTurns = worldState.CurrentTurn
    }else{
        worldStateChannel := make(chan WorldState)
        s.worldStateChannel <- worldStateChannel
        worldState := <-worldStateChannel
        respWorld = worldState.World
        respTurns = worldState.CurrentTurn
    }
    s.isPaused = !s.isPaused
    
    res.World = respWorld
    res.Turns = respTurns
    
    if s.isPaused {
        res.Message = "Paused"
    } else {
        res.Message = "Continuing"
    }
    return nil
}

// main initializes and starts the broker server
func main() {
	pAddr := flag.String("port", "8030", "Port to listen on")
    flag.Parse()
    rand.Seed(time.Now().UnixNano())
    rpc.Register(NewSecretStringOperations())
    
    var err error
    listener, err = net.Listen("tcp", "0.0.0.0:8030")
    if err != nil {
        fmt.Printf("Error starting server: %v\n", err)
        return
    }
    defer listener.Close()
    
    fmt.Printf("Server is listening on port %s...\n", *pAddr)
    
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

    <-shutdown
    fmt.Println("Server shutdown complete")
}
