package flock

// grid is a uniform spatial hash over the toroidal world. Buckets hold boid
// indices and are reused across rebuilds so steady-state ticks do not
// allocate.
type grid struct {
	cols, rows   int
	cellW, cellH float64
	w, h         float64
	buckets      [][]int32
}

// newGrid sizes cells to at least the requested cell edge (the max neighbor
// radius) so a 3×3 cell scan always covers a full query radius.
func newGrid(w, h, cell float64) *grid {
	cols := max(int(w/cell), 1)
	rows := max(int(h/cell), 1)
	return &grid{
		cols:    cols,
		rows:    rows,
		cellW:   w / float64(cols),
		cellH:   h / float64(rows),
		w:       w,
		h:       h,
		buckets: make([][]int32, cols*rows),
	}
}

func (g *grid) cellCoords(p Vec2) (int, int) {
	cx := int(p.X / g.cellW)
	cy := int(p.Y / g.cellH)
	// Guard float edge cases (p exactly at the far boundary).
	if cx < 0 {
		cx = 0
	} else if cx >= g.cols {
		cx = g.cols - 1
	}
	if cy < 0 {
		cy = 0
	} else if cy >= g.rows {
		cy = g.rows - 1
	}
	return cx, cy
}

// rebuild reindexes all boids, truncating (not freeing) existing buckets.
func (g *grid) rebuild(boids []Boid) {
	for i := range g.buckets {
		g.buckets[i] = g.buckets[i][:0]
	}
	for i := range boids {
		cx, cy := g.cellCoords(boids[i].Pos)
		idx := cy*g.cols + cx
		g.buckets[idx] = append(g.buckets[idx], int32(i))
	}
}

// neighbors appends to out the indices of boids within radius of boids[i]
// (torus distance, self excluded) and returns the extended slice. The 3×3
// cell scan wraps; on grids narrower than 3 cells, duplicate cell visits are
// skipped so no neighbor is reported twice.
func (g *grid) neighbors(boids []Boid, i int, radius float64, out []int32) []int32 {
	self := boids[i]
	cx, cy := g.cellCoords(self.Pos)
	r2 := radius * radius
	var visited [9]int
	nVisited := 0
scan:
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			nx := (cx + dx + g.cols) % g.cols
			ny := (cy + dy + g.rows) % g.rows
			cell := ny*g.cols + nx
			for k := 0; k < nVisited; k++ {
				if visited[k] == cell {
					continue scan
				}
			}
			visited[nVisited] = cell
			nVisited++
			for _, j := range g.buckets[cell] {
				if int(j) == i {
					continue
				}
				d := torusDelta(self.Pos, boids[j].Pos, g.w, g.h)
				if d.X*d.X+d.Y*d.Y <= r2 {
					out = append(out, j)
				}
			}
		}
	}
	return out
}
