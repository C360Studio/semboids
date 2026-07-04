package sim

import (
	"strconv"
	"time"

	"github.com/c360studio/semboids/internal/flock"
)

// Frame is one aggregated snapshot of the flock, published per tick. It
// marshals to the compact wire format consumed by the UI:
//
//	{"tick":N,"t":unixMillis,"w":W,"h":H,"boids":[[id,x,y,vx,vy],...]}
type Frame struct {
	Tick  uint64
	T     int64
	W, H  float64
	Boids []flock.Boid
}

// NewFrame snapshots the given boids (the slice is referenced, not copied;
// marshal before the next engine tick).
func NewFrame(tick uint64, t time.Time, p flock.Params, boids []flock.Boid) Frame {
	return Frame{
		Tick:  tick,
		T:     t.UnixMilli(),
		W:     p.Width,
		H:     p.Height,
		Boids: boids,
	}
}

// MarshalJSON hand-builds the compact array format: encoding/json cannot
// express per-boid [id,x,y,vx,vy] tuples from a struct, and this path runs
// every tick.
func (f Frame) MarshalJSON() ([]byte, error) {
	// ~40 bytes per boid plus header keeps growth to one allocation in
	// the common case.
	buf := make([]byte, 0, 64+len(f.Boids)*48)
	buf = append(buf, `{"tick":`...)
	buf = strconv.AppendUint(buf, f.Tick, 10)
	buf = append(buf, `,"t":`...)
	buf = strconv.AppendInt(buf, f.T, 10)
	buf = append(buf, `,"w":`...)
	buf = appendFloat(buf, f.W)
	buf = append(buf, `,"h":`...)
	buf = appendFloat(buf, f.H)
	buf = append(buf, `,"boids":[`...)
	for i, b := range f.Boids {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, '[')
		buf = strconv.AppendUint(buf, uint64(b.ID), 10)
		buf = append(buf, ',')
		buf = appendFloat(buf, b.Pos.X)
		buf = append(buf, ',')
		buf = appendFloat(buf, b.Pos.Y)
		buf = append(buf, ',')
		buf = appendFloat(buf, b.Vel.X)
		buf = append(buf, ',')
		buf = appendFloat(buf, b.Vel.Y)
		buf = append(buf, ']')
	}
	buf = append(buf, `]}`...)
	return buf, nil
}

// appendFloat matches encoding/json's shortest-round-trip float formatting.
func appendFloat(buf []byte, v float64) []byte {
	return strconv.AppendFloat(buf, v, 'g', -1, 64)
}
