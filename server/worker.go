package main

import (
	"flag"
	"math/rand"
	"net"
	"net/rpc"
	"time"
	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
	"fmt"
	"strings"
)

var (
    listener net.Listener
    shutdown = make(chan bool)
)

type SecretStringOperations struct {
    aliveCellsChannel chan chan GameState
    worldStateChannel chan chan WorldState
    stopChannel       chan bool
    pauseChannel      chan bool
    isPaused          bool
    gameRunning       bool
}

// Initialize channels when the struct is created
func NewSecretStringOperations() *SecretStringOperations {
    return &SecretStringOperations{
        aliveCellsChannel: make(chan chan GameState),
        worldStateChannel: make(chan chan WorldState),
        stopChannel:       make(chan bool),
        pauseChannel:      make(chan bool),
    }
}

type GameState struct {
    AliveCells int
    CurrentTurn int
}

type WorldState struct {
    World       [][]uint8
    CurrentTurn int
}

// Cell state constants
const (
    Dead  uint8 = 0
    Alive uint8 = 255
)


// nextState calculates the next state of the world according to Game of Life rules
func nextState(world [][]uint8, imageWidth, imageHeight int, region stubs.CoordinatePair) [][]uint8 {
    h, w := imageHeight, imageWidth
    sliceHeight := region.Y2 - region.Y1 + 1
    sliceWidth := region.X2 - region.X1 + 1

    sliceStartY := region.Y1
    sliceStartX := region.X1
    
    // Initialize new world state
    newWorldSlice := make([][]uint8, sliceHeight)
    for i := range newWorldSlice {
        newWorldSlice[i] = make([]uint8, sliceWidth)
    }

    // Update each cell based on Game of Life rules
    for y := sliceStartY; y <= region.Y2; y++ {
        for x := sliceStartX; x <= region.X2; x++ {
            neighbors := countLiveNeighbors(world, x, y, w, h)
            
            switch {
            case world[y][x] == Alive && (neighbors < 2 || neighbors > 3):
                newWorldSlice[y - sliceStartY][x - sliceStartX] = Dead
            case world[y][x] == Dead && neighbors == 3:
                newWorldSlice[y - sliceStartY][x - sliceStartX] = Alive
            }
        }
    }
    return newWorldSlice
}

func (s *SecretStringOperations) NextState(req stubs.WorkerRequest, res *stubs.Response) (err error) {
	world := nextState(req.World, req.ImageWidth, req.ImageHeight, req.Region)
	res.UpdatedWorld = world
	return nil
}

// AliveCellsCount is an RPC method that returns the current count of alive cells and turn number.
// It creates a response channel, sends it through the aliveCellsChannel to get the current game state,
// and populates the response with the number of alive cells and current turn number.
func (s *SecretStringOperations) AliveCellsCount(req stubs.AliveCellsCountRequest, res *stubs.AliveCellsCountResponse) (err error) {
	responseChannel := make(chan GameState)
	s.aliveCellsChannel <- responseChannel
	state := <-responseChannel
	res.CellsAlive = state.AliveCells
	res.Turns = state.CurrentTurn
	// func nextState(world [][]uint8, p gol.Params, c gol.DistributorChannels) [][]uint8
	return nil
}


func calculateAliveCells(world [][]byte, imageWidth int, imageHeight int) []util.Cell {
	alives := make([]util.Cell, 0)
	for y := 0; y < imageHeight; y++ {
		for x := 0; x < imageWidth; x++ {
			if world[y][x] == 255 {
				newCell := util.Cell{x, y}
				alives = append(alives, newCell)
			}
		}
	}
	return alives
}

// countLiveNeighbors counts the number of live neighbors for a given cell
func countLiveNeighbors(world [][]uint8, x, y, w, h int) int {
    // Neighbor positions relative to current cell
    neighbors := [][2]int{
        {-1, -1}, {-1, 0}, {-1, 1},
        {0, -1},           {0, 1},
        {1, -1},  {1, 0},  {1, 1},
    }
    
    count := 0
    for _, n := range neighbors {
        ny := ((y + n[0]) + h) % h
        nx := ((x + n[1]) + w) % w
        if world[ny][nx] == Alive {
            count++
        }
    }
    return count
}

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