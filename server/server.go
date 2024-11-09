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
	"os"
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
func nextState(world [][]uint8, turns, threads, imageWidth, imageHeight int) [][]uint8 {
    h, w := imageHeight, imageWidth
    
    // Initialize new world state
    newWorld := make([][]uint8, h)
    for i := range newWorld {
        newWorld[i] = make([]uint8, w)
        copy(newWorld[i], world[i])
    }

    // Update each cell based on Game of Life rules
    for y := 0; y < h; y++ {
        for x := 0; x < w; x++ {
            neighbors := countLiveNeighbors(world, x, y, w, h)
            
            switch {
            case world[y][x] == Alive && (neighbors < 2 || neighbors > 3):
                newWorld[y][x] = Dead
            case world[y][x] == Dead && neighbors == 3:
                newWorld[y][x] = Alive
            }
        }
    }
    return newWorld
}

// doAllTurns processes the Game of Life simulation, handling game state updates and control operations.
// It takes the initial world state, game parameters, and control channels as input.
// The function runs an infinite loop that:
// - Responds to pause/unpause commands through pauseChannel
// - Reports current alive cells count through aliveCellsChan
// - Reports current world state through worldStateChan
// - Handles stop commands through stopChannel
// - Calculates next world state when not paused
// Returns the final world state when stopped
func doAllTurns(world [][]uint8, turns int, threads int, imageWidth int, imageHeight int, aliveCellsChan chan chan GameState, worldStateChan chan chan WorldState, stopChannel chan bool, pauseChannel chan bool) [][]uint8 {
	isPaused := false
	fmt.Printf("Starting game\n")
	t := 0
	for{
        select {
			case responseChan := <-worldStateChan:
				fmt.Println("worldStateChan in case")
				fmt.Println("turn: ", t)
				state := WorldState{
					World: world,
					CurrentTurn: t,
				}
				responseChan <- state

			case shouldPause := <-pauseChannel:
				fmt.Println("pauseChannel in case")
				isPaused = shouldPause
				fmt.Printf("Paused: %t\n", isPaused)
			
			case responseChan := <-aliveCellsChan:
				fmt.Println("aliveCellsChan in case")
				state := GameState{
					AliveCells: len(calculateAliveCells(world, imageWidth, imageHeight)),
					CurrentTurn: t,
				}
				responseChan <- state
			
			case <-stopChannel:
				fmt.Printf("Stopping game\n")
				return world
			
			default:
				if !isPaused {
					world = nextState(world, turns, threads, imageWidth, imageHeight)
					t++
				}
        }
    }
	return world
}


// Start initializes the game channels and starts processing turns. It takes a Request containing
// the initial world state and game parameters, and returns the final world state in the Response.
// The function sets up channels for tracking alive cells, world state, stopping, and pausing the game.
func (s *SecretStringOperations) Start(req stubs.Request, res *stubs.Response) (err error) {
	fmt.Println("Starting game")
	/*
	s.aliveCellsChannel = make(chan chan GameState)
	s.worldStateChannel = make(chan chan WorldState)
	s.stopChannel = make(chan bool)
	s.pauseChannel = make(chan bool)
	*/
	s.isPaused = false
	s.gameRunning = true
	// Create a channel to signal when processing is complete
    completionChannel := make(chan [][]uint8)
    
    // Run doAllTurns in a goroutine
    go func() {
        result := doAllTurns(req.World, req.Turns, req.Threads, req.ImageWidth, req.ImageHeight, 
            s.aliveCellsChannel, s.worldStateChannel, s.stopChannel, s.pauseChannel)
        completionChannel <- result
    }()
    
    // Wait for the processing to complete
    res.UpdatedWorld = <-completionChannel
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


// State handles various game control commands received from the client:
// - "save": Captures and returns the current world state and turn number
// - "quit": Signals the game to stop execution
// - "pause": Toggles the pause state of the game, returning current state when pausing
// - "kill": Gracefully shuts down the server by closing connections and stopping games
func (s *SecretStringOperations) State(req stubs.StateRequest, res *stubs.StateResponse) (err error) {
	fmt.Printf("state request: %v\n", req.Command)
	switch req.Command {
		case "save":
			// TODO: Implement save functionality
			fmt.Println("SAVE")
			worldStateChannel := make(chan WorldState)
			s.worldStateChannel <- worldStateChannel
			worldState := <-worldStateChannel
			res.World = worldState.World
			res.Turns = worldState.CurrentTurn
		case "quit":
			// TODO: Implement quit functionality 
			fmt.Println("QUIT")
			worldStateChannel := make(chan WorldState)
			s.worldStateChannel <- worldStateChannel
			worldState := <-worldStateChannel
			res.Turns = worldState.CurrentTurn
			res.World = worldState.World
			/*
			if res.World != nil {
				fmt.Printf("World dimensions: %d x %d\n", len(res.World), len(res.World[0]))
			} else {
				fmt.Println("World is nil")
			}
			fmt.Println("Turns: ", res.Turns)
			*/
			s.stopChannel <- true
			return nil
		case "pause":
			//make sure the game has started
			if(s.worldStateChannel == nil){
				fmt.Println("Game has not started, but pause command received")
			}

			fmt.Println("PAUSE")
			fmt.Println("isPaused: ", s.isPaused)
			var respWorld [][]uint8
			var respTurns int
			
			if(!s.isPaused){
				fmt.Println("isPaused: ", s.isPaused)
				worldStateChannel := make(chan WorldState)
				fmt.Println("worldStateChannel created")
				s.worldStateChannel <- worldStateChannel
				fmt.Println("worldStateChannel sent")
				worldState := <-worldStateChannel
				fmt.Println("worldState received")
				respWorld = worldState.World
				respTurns = worldState.CurrentTurn
			}else{
				respWorld = nil
				respTurns = 0
			}

			s.isPaused = !s.isPaused
			s.pauseChannel <- s.isPaused
			
			res.World = respWorld
			res.Turns = respTurns
			
			if s.isPaused {
				res.Message = "Paused"
			} else {
				res.Message = "Continuing"
			}
		case "kill":
			fmt.Println("Kill")
			fmt.Println("Shutting down server...")
			// Close the listener to stop accepting new connections
			if listener != nil {
				listener.Close()
			}

			// Signal any running games to stop
			s.stopChannel <- true
			// Give a small delay for cleanup
			//time.Sleep(100 * time.Millisecond)
			defer os.Exit(0)
		default:
			fmt.Printf("Unknown command: %s\n", req.Command)
	}
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