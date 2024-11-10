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

// Global variables for server operation
var (
    listener net.Listener
    shutdown = make(chan bool)
)

// SecretStringOperations handles RPC methods and maintains game state
type SecretStringOperations struct {
    aliveCellsChannel chan chan GameState
    worldStateChannel chan chan WorldState
    stopChannel       chan bool
    pauseChannel      chan bool
    isPaused          bool
    gameRunning       bool
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

// GameState represents the current state of the game including alive cells and turn number
type GameState struct {
    AliveCells int
    CurrentTurn int
}

// WorldState represents the complete state of the game world
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
func nextState(world [][]uint8, region stubs.CoordinatePair) [][]uint8 {
    h, w := len(world), len(world[0])
    
    // Initialize new world state without halo region
    newWorld := make([][]uint8, h-2)
    for i := range newWorld {
        newWorld[i] = make([]uint8, w-2)
    }

    // Update each cell based on Game of Life rules, excluding halo region
    for y := 1; y < h-1; y++ {
        for x := 1; x < w-1; x++ {
            neighbors := countLiveNeighbors(world, x, y, w, h)
            
            switch {
                case world[y][x] == Alive && (neighbors < 2 || neighbors > 3):
                    newWorld[y-1][x-1] = Dead
                case world[y][x] == Dead && neighbors == 3:
                    newWorld[y-1][x-1] = Alive
                default:
                    newWorld[y-1][x-1] = world[y][x]
            }
        }
    }
    return newWorld
}

// NextState is an RPC method that processes the next state for a given world region
func (s *SecretStringOperations) NextState(req stubs.WorkerRequest, res *stubs.Response) (err error) {
	world := nextState(req.World, req.Region)
	res.UpdatedWorld = world
	return nil
}

// AliveCellsCount is an RPC method that returns the current count of alive cells and turn number
func (s *SecretStringOperations) AliveCellsCount(req stubs.AliveCellsCountRequest, res *stubs.AliveCellsCountResponse) (err error) {
	responseChannel := make(chan GameState)
	s.aliveCellsChannel <- responseChannel
	state := <-responseChannel
	res.CellsAlive = state.AliveCells
	res.Turns = state.CurrentTurn
	return nil
}

// calculateAliveCells returns a slice of Cell structs representing all alive cells in the world
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

// main initializes and runs the RPC server
func main() {
	pAddr := flag.String("port", "8030", "Port to listen on")
    flag.Parse()
    rand.Seed(time.Now().UnixNano())
    rpc.Register(NewSecretStringOperations())
    
    var err error
    listener, err = net.Listen("tcp", "0.0.0.0:8030")
    if err != nil {
        return
    }
    defer listener.Close()
    
    fmt.Printf("Server is listening on port %s...\n", *pAddr)
    
    // Handle incoming connections in a separate goroutine
    go func() {
        for {
            select {
            case <-shutdown:
                return
            default:
                conn, err := listener.Accept()
                if err != nil {
                    if !strings.Contains(err.Error(), "use of closed network connection") {
                        return
                    }
                    return
                }
                go rpc.ServeConn(conn)
            }
        }
    }()

    // Wait for shutdown signal
    <-shutdown
}