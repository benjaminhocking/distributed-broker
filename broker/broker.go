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
    "sync"
)

var (
	listener net.Listener
	shutdown = make(chan bool)
    instances = []string{"44.203.136.187", "44.203.194.31", "3.86.38.209", "54.164.86.590"}
)


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
    Client *rpc.Client
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

func getWorldRegion(world [][]uint8, region stubs.CoordinatePair) [][]uint8{
    // this function returns a slice of the world that corresponds to the region for this worker
    // it includes the halo region around the area.
    // Calculate dimensions of the region including halo
    height := region.Y2 - region.Y1 + 3  // +3 for halo (1 above, 1 below)
    width := region.X2 - region.X1 + 3   // +3 for halo (1 left, 1 right)

    
    // Create slice to hold the region
    regionSlice := make([][]uint8, height)
    for i := range regionSlice {
        regionSlice[i] = make([]uint8, width)
    }

    worldHeight := len(world)
    worldWidth := len(world[0])

    // Copy region data including halo
    for y := 0; y < height; y++ {
        for x := 0; x < width; x++ {
            // Calculate world coordinates with wrapping
            worldY := ((region.Y1 - 1 + y) + worldHeight) % worldHeight
            worldX := ((region.X1 - 1 + x) + worldWidth) % worldWidth
            regionSlice[y][x] = world[worldY][worldX]
        }
    }
    //fmt.Printf("getWorldRegion for region: %v\n", region)
    //fmt.Printf("regionSlice: \n")
    //for _, row := range regionSlice {
    //    fmt.Printf("region %v,%v-%v,%v row: %v\n", region.Y1, region.X1, region.Y2, region.X2, row)
    //}

    return regionSlice

}

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
    fmt.Printf("Workers: %v\n", workers)

    fmt.Printf("world: \n")
    for _, row := range req.World {
        fmt.Println(row)
    }

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
            
            world = mergeWorldSlices(worldSlices, world)
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

func mergeWorldSlices(worldSlices []WorldSlice, world [][]uint8) [][]uint8 {
    if len(worldSlices) == 0 {
        return nil
    }

    // Determine total dimensions
    totalHeight := 0
    width := worldSlices[0].Region.X2 - worldSlices[0].Region.X1 + 1
    for _, slice := range worldSlices {
        if slice.Region.Y2+1 > totalHeight {
            totalHeight = slice.Region.Y2 + 1
        }
    }

    // Initialize merged world
    mergedWorld := make([][]uint8, totalHeight)
    for i := range mergedWorld {
        mergedWorld[i] = make([]uint8, width)
        copy(mergedWorld[i], world[i]) // Copy existing world state
    }

    // Use wait group to synchronize goroutines
    var wg sync.WaitGroup

    // Copy slices concurrently, excluding halo regions
    for _, slice := range worldSlices {
        //fmt.Printf("Slice size - Height: %d, Width: %d\n", len(slice.World), len(slice.World[0]))
        wg.Add(1)
        go func(ws WorldSlice) {
            defer wg.Done()
            region := ws.Region
            startY := region.Y1
            startX := region.X1

            // Copy only the non-halo region
            for y := 0; y <= len(ws.World)-1; y++ {
                for x := 0; x <= len(ws.World[0])-1; x++ {
                    mergedWorld[startY+y][startX+x] = ws.World[y][x]
                }
            }
        }(slice)
    }

    // Wait for all goroutines to finish
    wg.Wait()

    //fmt.Printf("Merged world dimensions - Height: %d, Width: %d\n", len(mergedWorld), len(mergedWorld[0]))

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
