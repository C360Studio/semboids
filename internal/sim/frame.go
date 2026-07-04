package sim

import (
	"strconv"
	"time"

	"github.com/c360studio/semboids/internal/flock"
	"github.com/c360studio/semboids/internal/zone"
)

// Frame is one aggregated snapshot of the flock, published per tick. It
// marshals to the compact wire format consumed by the UI:
//
//	{"tick":N,"t":unixMillis,"w":W,"h":H,
//	 "zones":[[type,x,y,r],...],
//	 "boids":[[id,x,y,vx,vy,m],...]}
//
// where m is the boid's active modifier kind (0 none, 1 flee, 2 attract,
// 3 wind).
type Frame struct {
	Tick  uint64
	T     int64
	W, H  float64
	Zones []zone.Zone
	Boids []flock.Boid
	Mods  []uint8
}

// NewFrame snapshots the given boids (slices are referenced, not copied;
// marshal before the next engine tick). mods is aligned with boids; nil
// means no active modifiers.
func NewFrame(
	tick uint64, t time.Time, p flock.Params,
	boids []flock.Boid, zones []zone.Zone, mods []uint8,
) Frame {
	return Frame{
		Tick:  tick,
		T:     t.UnixMilli(),
		W:     p.Width,
		H:     p.Height,
		Zones: zones,
		Boids: boids,
		Mods:  mods,
	}
}

// MarshalJSON hand-builds the compact array format: encoding/json cannot
// express the per-boid and per-zone tuples from a struct, and this path
// runs every tick.
func (f Frame) MarshalJSON() ([]byte, error) {
	buf := make([]byte, 0, 96+len(f.Zones)*32+len(f.Boids)*52)
	buf = append(buf, `{"tick":`...)
	buf = strconv.AppendUint(buf, f.Tick, 10)
	buf = append(buf, `,"t":`...)
	buf = strconv.AppendInt(buf, f.T, 10)
	buf = append(buf, `,"w":`...)
	buf = appendFloat(buf, f.W)
	buf = append(buf, `,"h":`...)
	buf = appendFloat(buf, f.H)
	buf = append(buf, `,"zones":[`...)
	for i, z := range f.Zones {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, `["`...)
		buf = append(buf, z.Type...)
		buf = append(buf, `",`...)
		buf = appendFloat(buf, z.X)
		buf = append(buf, ',')
		buf = appendFloat(buf, z.Y)
		buf = append(buf, ',')
		buf = appendFloat(buf, z.R)
		buf = append(buf, ']')
	}
	buf = append(buf, `],"boids":[`...)
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
		buf = append(buf, ',')
		var m uint8
		if i < len(f.Mods) {
			m = f.Mods[i]
		}
		buf = strconv.AppendUint(buf, uint64(m), 10)
		buf = append(buf, ']')
	}
	buf = append(buf, `]}`...)
	return buf, nil
}

// appendFloat matches encoding/json's shortest-round-trip float formatting.
func appendFloat(buf []byte, v float64) []byte {
	return strconv.AppendFloat(buf, v, 'g', -1, 64)
}
