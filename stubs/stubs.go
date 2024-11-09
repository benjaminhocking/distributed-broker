package stubs


var ReverseHandler = "SecretStringOperations.Reverse"
var PremiumReverseHandler = "SecretStringOperations.FastReverse"
var StateHandler = "SecretStringOperations.State"

type CoordinatePair struct{
    X1, Y1 int
    X2, Y2 int
}

type Response struct {
	UpdatedWorld [][]uint8
}

type Request struct {
	World [][]uint8
	Turns       int
	Threads     int
	ImageWidth  int
	ImageHeight int
}

type BrokerRequest struct {
	World [][]uint8
	Turns       int
	Threads     int
	ImageWidth  int
	ImageHeight int
	Workers 	int
}

type WorkerRequest struct{
	World	 [][]uint8
	Region	 CoordinatePair
	ImageWidth int
	ImageHeight int
}

type AliveCellsCountRequest struct {}

type AliveCellsCountResponse struct {
	CellsAlive int
	Turns int
}

type StateRequest struct {
	Command string
}

type StateResponse struct {
	World [][]uint8
	Turns int
	Message string
}