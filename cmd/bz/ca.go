package main

import (
	"image"
	"math/rand"
)

/*
 * implents cellular automata to produce patterns similar
 * to that found in Belusov-Zabotinsky reactions, slime mold
 * agglomerations, and other excitable media.
 * based on implementation found in "Spiral symmetry" edited
 * by Hargittai and Pickover.
 */

type BZ struct {
	nstates      int
	cells        []Cell
	n            int
	r            int
	m0           int
	xsize, ysize int
}

type Cell struct {
	s         uint8 // state of cell, in [0..N+2)
	s1        uint8 // sigma - intermediate state
	neighbors []*Cell
	x, y      float32 // random point within cell (only used to calculate neighbours)
}

func NewBZ(xsize, ysize, n, r, m0 int) *BZ {
	bz := &BZ{
		xsize: xsize,
		ysize: ysize,
		n:     n,
		r:     r,
		m0:    m0,
		cells: make([]Cell, xsize*ysize),
	}
	bz.assignPositions()
	bz.calcNeighbors()
	return bz
}

func (bz *BZ) NStates() int {
	return bz.n + 2
}

func (bz *BZ) assignPositions() {
	for i := range bz.cells {
		cell := &bz.cells[i]
		cell.x = rand.Float32()
		cell.y = rand.Float32()
	}
}

func (bz *BZ) Bounds() image.Rectangle {
	return image.Rect(0, 0, bz.xsize, bz.ysize)
}

func (bz *BZ) Set(x, y int, s uint8) {
	bz.cells[x+y*bz.xsize].s = s
}

func (bz *BZ) calcNeighbors() {
	diam := bz.r*2 + 1 // max diameter of circle, in cells
	r2 := float32(bz.r * bz.r)

	neighbors := make([]*Cell, diam*diam)
	for y := 0; y < bz.ysize; y++ {
		for x := 0; x < bz.xsize; x++ {
			curr := &bz.cells[y*bz.xsize+x]
			xpos := float32(x) + curr.x
			ypos := float32(y) + curr.y
			neighbors = neighbors[:0]
			for j := y - bz.r; j <= y+bz.r; j++ {
				for i := x - bz.r; i <= x+bz.r; i++ {
					if j < 0 || j >= bz.ysize || i < 0 || i >= bz.xsize {
						continue
					}
					ncell := &bz.cells[j*bz.xsize+i]
					xdelta := float32(i) + ncell.x - xpos
					ydelta := float32(j) + ncell.y - ypos
					dist2 := xdelta*xdelta + ydelta*ydelta
					if dist2 < r2 {
						neighbors = append(neighbors, ncell)
					}
				}
			}
			if len(neighbors) > 0 {
				curr.neighbors = make([]*Cell, len(neighbors))
				copy(curr.neighbors, neighbors)
			}
		}
	}
}

func (bz *BZ) Step() {
	for i := range bz.cells {
		bz.cellStep1(&bz.cells[i])
	}
	for i := range bz.cells {
		bz.cellStep2(&bz.cells[i])
	}
}

func (bz *BZ) cellStep1(cell *Cell) {
	s := cell.s
	v := 0
	lim := uint8(bz.n + 1)
	for _, neighbor := range cell.neighbors {
		if neighbor.s == lim {
			v++
		}
	}
	switch {
	case s > 0:
		cell.s1 = s - 1
	case v >= bz.m0:
		cell.s1 = lim
	default:
		cell.s1 = 0
	}
}

func (bz *BZ) cellStep2(cell *Cell) {
	sigma := cell.s1
	if sigma == 0 || sigma == uint8(bz.n+1) {
		cell.s = sigma
	} else {
		sum := 0
		for _, neighbor := range cell.neighbors {
			sum += int(neighbor.s1)
		}
		cell.s = uint8(float32(sum)/float32(len(cell.neighbors)) + 1)
	}
}
