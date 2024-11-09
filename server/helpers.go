package main



// Cell state constants
const (
    Dead  uint8 = 0
    Alive uint8 = 255
)

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

// nextState calculates the next state of the world according to Game of Life rules
func nextStateN(world [][]uint8, turns, threads, imageWidth, imageHeight int) [][]uint8 {
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