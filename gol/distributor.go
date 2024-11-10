package gol

// acts as the client
import (
	"fmt"
	"uk.ac.bris.cs/gameoflife/util"
	"uk.ac.bris.cs/gameoflife/stubs"
	"net/rpc"
	"sync"
	"time"
)

type DistributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	IoInput    <-chan uint8
	keyPresses <- chan rune
}


var (
    rpcClient *rpc.Client
    clientMu  sync.Mutex
    once      sync.Once
)

// Manage a singleton RPC connection
func getRPCClient() (*rpc.Client, error) {
    var err error
    once.Do(func() {
        rpcClient, err = rpc.Dial("tcp", "3.86.38.167:8030")
    })
    
    if err != nil {
		fmt.Printf("Error: %v\n", err)
        return nil, err
    }
    return rpcClient, nil
}



func distributor(p Params, c DistributorChannels) {
	fmt.Println("distributor")

	H := p.ImageHeight
	W := p.ImageWidth


	// Create variable for world
	world := make([][]uint8, H)
	for i := 0; i < H; i++ {
		world[i] = make([]uint8, W) //Initialise each row
	}

	// Send the ioInput command on the command channel
	// This tells the io code to read the pdm image
	c.ioCommand <- ioInput


	// Construct the filename following the image name convention
	filename := fmt.Sprintf("%dx%d", p.ImageHeight, p.ImageWidth)
	
	// Send the filename on the filename channel
	c.ioFilename <- filename

	// Recieve the pixel values from the io input channel
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			world[y][x] = <-c.IoInput
		}
	}
	
	// Wait until all data is written before continuing
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	// Initialise helper channels and vars

	completedTurns := 0
	writeToFile := true
	turn := 0

	// Channel to signal when all turns are complete
	done := make(chan bool)

	// Channel to enable communication of quit
	quit := make(chan bool)


	// Initialise Ticker

	// Ticker to report alive cell counts every 2 seconds
	ticker := time.NewTicker(2 * time.Second)

	// Stop the ticker once distributor has exited



	// Ticker logic to report alive cells count every 2 seconds
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Optional: log the recovered error
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
							// Safely send event only if not quitting
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


	// Signal that execution is starting
	c.events <- StateChange{turn, Executing}
	

    // Start goroutine to handle keypresses
    go func() {
        for {
            select {
            case key := <-c.keyPresses:
				fmt.Println("Key pressed: ", key)
                switch key {
                case 's':
					saveBoardState(p, c)
				
                case 'q':
					quitClient(p, c)
					writeToFile = true
					fmt.Printf("writeToFile = %t\n", writeToFile)
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

	fmt.Println("before world")
	fmt.Println("writeToFile: ", writeToFile)
	world, _ = doAllTurnsBroker(world, p)
	fmt.Println("after world")
	if(world == nil){
		fmt.Println("world is nil")
		close(c.events)
		return
	}

	fmt.Println("1")


	// TODO: Report the final state using FinalTurnCompleteEvent.
	alives := calculateAliveCells(world)
	c.events <- FinalTurnComplete{CompletedTurns: completedTurns, Alive: alives}
	fmt.Println("2")
	// send an event down an events channel
	// must implement the events channel, FinalTurnComplete is an event so must implement the event interface
	// Make sure that the Io has finished any output before exiting.
	fmt.Println(writeToFile)
	if(writeToFile){
		fmt.Println("writeToFile")
		//output the state of the board after all turns have been completed as a PGM image
		c.ioCommand <- ioOutput
		filename = fmt.Sprintf("%dx%dx%d", p.ImageHeight, p.ImageWidth, p.Turns)
		fmt.Println("filename: ", filename)
		c.ioFilename <- filename
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				c.ioOutput <- world[y][x]
			}
		}
		c.events <- ImageOutputComplete{completedTurns, filename}
	}

	// if it's idle it'll return true so you can use it before reading input, for example
	// to ensure output has saved before reading
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}

	ticker.Stop()
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	fmt.Println("closing events")
	close(c.events)
	fmt.Println("events closed")
}

func pause(p Params,c DistributorChannels, completedTurns int) (int){
	fmt.Println("Pausing")

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
		fmt.Printf("Error stack trace:\n%+v\n", err)
		return completedTurns
	}
}

func quitClient(p Params, c DistributorChannels){

	fmt.Println("Quitting")

	client, err := getRPCClient()
	
	if err == nil {
		request := stubs.StateRequest{Command: "quit"}
		fmt.Println("request sent")

		response := new(stubs.StateResponse)

		client.Call("SecretStringOperations.Quit", request, response)

		fmt.Println("response received")

		//fmt.Println("response: ", response)
		fmt.Println("Turns: ", response.Turns)

		if response.World != nil {
			fmt.Printf("World dimensions: %d x %d\n", len(response.World), len(response.World[0]))
		} else {
			fmt.Println("World is nil")
		}

		alives := calculateAliveCells(response.World)

		c.events <- FinalTurnComplete{CompletedTurns: response.Turns, Alive: alives}

		fmt.Println("before state change quit")
		c.events <- StateChange{response.Turns, Quitting}
		fmt.Println("after state change quit")


		//saveBoardWorld(c, p, response.World, response.Turns)
		
	}else{
		fmt.Println("Failed to connect to server")
		fmt.Printf("Error stack trace:\n%+v\n", err)
	}
}

func kill(p Params,c DistributorChannels){
	
	fmt.Println("Killing server")

	client, err := getRPCClient()

	if err == nil {
		saveBoardState(p, c)

		request := stubs.StateRequest{Command: "kill"}

		response := new(stubs.StateResponse)

		client.Call("SecretStringOperations.Kill", request, response)

		fmt.Printf("Server killed, world state saved\n")
	}
}


/*
func main() {
	// connect to RPC server and send a request
	server := flag.String("server", "127.0.0.1:8030", "IP:port string to connect to as server")
	flag.Parse()
	fmt.Println("Server: ", *server)
	client, _ := rpc.Dial("tcp", *server)
	defer client.Close()
	request := stubs.Request{World: world, P: p, C: c}
	response := new(stubs.Response)
	client.Call(stubs.ReverseHandler, request, response)
	fmt.Println("Responded: ")
}
*/

func saveBoardState(p Params, c DistributorChannels) {
	client, err := getRPCClient()
	if err != nil {
		fmt.Printf("RPC failed: %v\n", err)
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
	
	// Send the current world state
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- world[y][x]
		}
	}
	
	// Wait for IO to complete
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	
	// Send ImageOutputComplete event
	c.events <- ImageOutputComplete{turns, filename}	
}

func saveBoardWorld(c DistributorChannels, p Params, world [][]uint8, turns int) {
	fmt.Println("saveBoardWorld")
	c.ioCommand <- ioOutput
	filename := fmt.Sprintf("%dx%dx%d", p.ImageHeight, p.ImageWidth, turns)
	c.ioFilename <- filename
	
	// Send the current world state
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- world[y][x]
		}
	}
	
	// Wait for IO to complete
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	
	// Send ImageOutputComplete event
	c.events <- ImageOutputComplete{turns, filename}	
}

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

func doAllTurnsBroker(world [][]uint8, p Params) ([][]uint8, int) {
	// Connect to RPC server
	client, err := getRPCClient()//rpc.Dial("tcp", "localhost:8030")
	if err != nil {
		// If we can't connect, fall back to local processing
		fmt.Printf("RPC failed: %v\n", err)
		return nil, 0
	}

	fmt.Println("Sending request to broker")

	workers := 5


	// Create request with current world state and parameters
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
		fmt.Printf("Server connection closed\n")
		fmt.Println("error: ", err)
		return response.UpdatedWorld, response.Turns
	}

	fmt.Println("exiting doAllTurnsBroker")

	return response.UpdatedWorld, response.Turns
}

// nextState calculates the next state of the board
func nextState(world [][]uint8, p Params, c DistributorChannels) [][]uint8 {

	H := p.ImageHeight
	W := p.ImageWidth

	// Create the new world state
	toReturn := make([][]uint8, H) // create a slice with rows equal to ImageHeight
	for i := 0; i < H; i++ {
		toReturn[i] = make([]uint8, W) // initialise each row with columns equal to ImageWidth
	}

	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			sum := countAliveNeighbours(world, x, y, W, H)

			if world[y][x] == 255 {
				// The cell was previously alive
				if sum < 2 || sum > 3 {
					toReturn[y][x] = 0
				} else if sum == 2 || sum == 3 {
					// Keep the cell alive
					toReturn[y][x] = 255
				}
			} else if world[y][x] == 0 {
				// The cell was previously dead
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

// calculateAliveCells returns a list of coordinates for cells that are alive
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