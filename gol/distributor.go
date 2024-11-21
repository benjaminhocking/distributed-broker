package gol

// Package gol implements the Game of Life distributed system with client-server architecture

import (
	"fmt"
	"uk.ac.bris.cs/gameoflife/util"
	"uk.ac.bris.cs/gameoflife/stubs"
	"net/rpc"
	"sync"
	"time"
)

// DistributorChannels holds all channels used for communication between components
type DistributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	IoInput    <-chan uint8
	keyPresses <- chan rune
}

// Global variables for managing RPC client connection
var (
    rpcClient *rpc.Client
    clientMu  sync.Mutex
    once      sync.Once
)

// getRPCClient creates or returns existing RPC connection using singleton pattern
func getRPCClient() (*rpc.Client, error) {
    var err error
    once.Do(func() {
	    rpcClient, err = rpc.Dial("tcp", "3.82.156.31:8030") // This is the IP of the broker
    })
    
    if err != nil {
		fmt.Printf("Error: %v\n", err)
        return nil, err
    }
    return rpcClient, nil
}

// distributor coordinates the game simulation and handles communication between components
func distributor(p Params, c DistributorChannels) {
	H := p.ImageHeight
	W := p.ImageWidth

	world := make([][]uint8, H)
	for i := 0; i < H; i++ {
		world[i] = make([]uint8, W)
	}

	c.ioCommand <- ioInput

	filename := fmt.Sprintf("%dx%d", p.ImageHeight, p.ImageWidth)
	c.ioFilename <- filename

	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			world[y][x] = <-c.IoInput
		}
	}
	
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	completedTurns := 0
	writeToFile := true
	turn := 0

	done := make(chan bool)
	quit := make(chan bool)

	ticker := time.NewTicker(2 * time.Second)

	// Goroutine for periodic reporting of alive cells
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("Recovered from panic in ticker:", r)
				return
			}
		}()
		for {
			select {
				case <-ticker.C:
					select {
						case <-quit:
							return
						default:
							alives, turns := calculateAliveCellsNode()
							completedTurns = turns
							select {
								case <-quit:
									return
								default:
									c.events <- AliveCellsCount{CompletedTurns: turns, CellsCount: alives}
							}
					}
				case <-done:
					return
				case <-quit:
					return
			}
		}
	}()

	c.events <- StateChange{turn, Executing}

	// Goroutine for handling keyboard input
    go func() {
        for {
            select {
            case key := <-c.keyPresses:
                switch key {
                case 's':
					saveBoardState(p, c)
				
                case 'q':
					completedTurns = quitClient(p, c)
					writeToFile = true
					quit <- true
					done <- true
					return

				case 'k':
					kill(p, c)
					writeToFile = false
					fmt.Println(writeToFile)
					quit <- true
					done <- true
					return
				
                case 'p':
					completedTurns = pause(p, c, completedTurns)
                }
            case <-quit:
                return
            }
        }
    }()

	var finalTurns int
	world, finalTurns = doAllTurnsBroker(world, p)
	if(world == nil){
		close(c.events)
		return
	}

	if(writeToFile){
		alives := calculateAliveCells(world)
		c.events <- FinalTurnComplete{CompletedTurns: finalTurns, Alive: alives}
		c.ioCommand <- ioOutput
		filename = fmt.Sprintf("%dx%dx%d", p.ImageHeight, p.ImageWidth, finalTurns)
		c.ioFilename <- filename
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				c.ioOutput <- world[y][x]
			}
		}
		c.ioCommand <- ioCheckIdle
		<-c.ioIdle
		
		c.events <- ImageOutputComplete{finalTurns, filename}
	}else{
		alives := calculateAliveCells(world)
		c.events <- FinalTurnComplete{CompletedTurns: finalTurns, Alive: alives}
	}

	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}

	ticker.Stop()
	
	close(c.events)
}

// pause sends pause command to server and handles state changes
func pause(p Params,c DistributorChannels, completedTurns int) (int){

	client, err := getRPCClient()

	if err == nil {
		request := stubs.StateRequest{Command: "pause"}
		response := new(stubs.StateResponse)

		client.Call("SecretStringOperations.Pause", request, response)

		completedTurns = response.Turns
		if response.Message == "Continuing"{
			c.events <- StateChange{response.Turns, Executing}
			fmt.Println("Continuing")
			return response.Turns
		}else{
			fmt.Printf("Turns: %d\n", response.Turns)
			c.events <- StateChange{response.Turns, Paused}
			return response.Turns
		}
	}else{
		fmt.Println("Failed to connect to server")
		return completedTurns
	}
}

// quitClient handles graceful shutdown of client connection
func quitClient(p Params, c DistributorChannels) int{
	fmt.Println("Quitting")

	client, err := getRPCClient()
	
	if err == nil {
		request := stubs.StateRequest{Command: "quit"}

		response := new(stubs.StateResponse)

		client.Call("SecretStringOperations.Quit", request, response)

		/*
		if response.World != nil {
			fmt.Printf("World dimensions: %d x %d\n", len(response.World), len(response.World[0]))
		} else {
			fmt.Println("World is nil")
		}
		*/

		return response.Turns
		
	}else{
		return 0
	}
}

// kill sends termination signal to server after saving state
func kill(p Params,c DistributorChannels){

	client, err := getRPCClient()

	if err == nil {
		saveBoardState(p, c)

		request := stubs.StateRequest{Command: "kill"}

		response := new(stubs.StateResponse)

		client.Call("SecretStringOperations.Kill", request, response)

	}
}

// saveBoardState requests current board state from server and saves it
func saveBoardState(p Params, c DistributorChannels) {
	client, err := getRPCClient()
	if err != nil {
		return
	}
	request := stubs.StateRequest{Command: "save"}
	response := new(stubs.StateResponse)
	client.Call("SecretStringOperations.Save", request, response)
	world := response.World
	turns := response.Turns

	c.ioCommand <- ioOutput
	filename := fmt.Sprintf("%dx%dx%d", p.ImageHeight, p.ImageWidth, turns)
	c.ioFilename <- filename
	
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- world[y][x]
		}
	}
	
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	
	c.events <- ImageOutputComplete{turns, filename}	
}

// saveBoardWorld saves given world state to file
func saveBoardWorld(c DistributorChannels, p Params, world [][]uint8, turns int) {
	c.ioCommand <- ioOutput
	filename := fmt.Sprintf("%dx%dx%d", p.ImageHeight, p.ImageWidth, turns)
	c.ioFilename <- filename
	
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- world[y][x]
		}
	}
	
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	
	c.events <- ImageOutputComplete{turns, filename}	
}

// calculateAliveCellsNode requests alive cells count from server
func calculateAliveCellsNode() (int, int) {
	client, err := getRPCClient()
	if err != nil {
		fmt.Printf("RPC failed: %v\n", err)
		return 0, 0
	}
	request := stubs.AliveCellsCountRequest{}
	response := new(stubs.AliveCellsCountResponse)
	client.Call("SecretStringOperations.AliveCellsCount", request, response)
	return response.CellsAlive, response.Turns
}

// doAllTurnsBroker sends world state to broker for distributed processing
func doAllTurnsBroker(world [][]uint8, p Params) ([][]uint8, int) {
	client, err := getRPCClient()
	if err != nil {
		fmt.Printf("RPC failed: %v\n", err)
		return nil, 0
	}
	fmt.Println("test")

	workers := 5

	request := stubs.BrokerRequest{
		World: world,
		Turns: p.Turns,
		Threads: p.Threads,
		ImageWidth: p.ImageWidth,
		ImageHeight: p.ImageHeight,
		Workers: workers,
	}
	response := new(stubs.Response)

	err = client.Call("SecretStringOperations.Start", request, response)
	if err != nil {
		return response.UpdatedWorld, response.Turns
	}


	return response.UpdatedWorld, response.Turns
}

// nextState calculates the next state of the world according to Game of Life rules
func nextState(world [][]uint8, p Params, c DistributorChannels) [][]uint8 {
	H := p.ImageHeight
	W := p.ImageWidth

	toReturn := make([][]uint8, H)
	for i := 0; i < H; i++ {
		toReturn[i] = make([]uint8, W)
	}

	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			sum := countAliveNeighbours(world, x, y, W, H)

			if world[y][x] == 255 {
				if sum < 2 || sum > 3 {
					toReturn[y][x] = 0
				} else if sum == 2 || sum == 3 {
					toReturn[y][x] = 255
				}
			} else if world[y][x] == 0 {
				if sum == 3 {
					toReturn[y][x] = 255
				} else {
					toReturn[y][x] = 0
				}
			}
		}
	}
	return toReturn
}

// countAliveNeighbours counts alive neighbors of a cell considering periodic boundaries
func countAliveNeighbours(world [][]uint8, x, y, width, height int) int {
	sum := 0
	
	directions := []struct{ dx, dy int }{
		{-1, -1}, {-1, 0}, {-1, 1},
		{0, -1},           {0, 1},
		{1, -1}, {1, 0}, {1, 1},
	}

	for _, d := range directions {
		nx, ny := (x+d.dx+width)%width, (y+d.dy+height)%height
		if world[ny][nx] == 255 {
			sum++
		}
	}

	return sum
}

// calculateAliveCells returns slice of Cell coordinates that are currently alive
func calculateAliveCells(world [][]uint8) []util.Cell {
	alives := make([]util.Cell, 0)
	for y := 0; y < len(world); y++ {
		for x := 0; x < len(world[y]); x++ {
			if world[y][x] == 255 {
				alives = append(alives, util.Cell{X: x, Y: y})
			}
		}
	}
	return alives
}
