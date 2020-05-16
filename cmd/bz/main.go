package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

var fillFlag = flag.String("fill", "rand", "initial state; one of rand, spiral[1234]")

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: bz [flags] [xsize ysize]\n")
		flag.PrintDefaults()
		os.Exit(2)
	}
	flag.Parse()
	rand.Seed(time.Now().UnixNano())
	filler := fillers[*fillFlag]
	if filler == nil {
		fmt.Fprintf(os.Stderr, "unknown filler %q\n", *fillFlag)
		flag.Usage()
	}
	xsize, ysize := 500, 500
	args := flag.Args()
	if len(args) > 0 {
		if len(args) != 2 {
			flag.Usage()
		}
		var err error
		xsize, err = strconv.Atoi(args[0])
		if err != nil {
			flag.Usage()
		}
		ysize, err = strconv.Atoi(args[1])
		if err != nil {
			flag.Usage()
		}
	}
	imgc := make(chan draw.Image, 1)
	go renderer(imgc, xsize, ysize, 3, filler)
	go func() {
		w := app.NewWindow(app.Size(unit.Dp(float32(xsize)), unit.Dp(float32(ysize))))
		if err := loop(w, imgc); err != nil {
			log.Fatal(err)
		}
	}()
	app.Main()
}

const frameInterval = time.Second / 60

func loop(w *app.Window, imgc <-chan draw.Image) error {
	gtx := new(layout.Context)
	var img draw.Image
	tick := time.NewTicker(frameInterval)
	imgc0 := imgc
	for {
		select {
		case e := <-w.Events():
			switch e := e.(type) {
			case system.DestroyEvent:
				return e.Err
			case system.FrameEvent:
				gtx.Reset(e.Queue, e.Config, e.Size)
				if img != nil {
					imgOp := paint.NewImageOp(img)
					imgOp.Add(gtx.Ops)
					paint.PaintOp{
						Rect: f32Rect(imgOp.Rect),
					}.Add(gtx.Ops)
				}
				e.Frame(gtx.Ops)
			}
		case <-tick.C:
			// Let the simulation proceed.
			imgc = imgc0
		case newImg := <-imgc:
			img = newImg
			w.Invalidate()
			// Don't let the simulation run faster than the tick rate.
			imgc = nil
		}
	}
}

func renderer(imgc chan<- draw.Image, xsize, ysize, width int, fill func(*BZ, int)) {
	const n = 6
	size := image.Pt(xsize, ysize)
	bz := NewBZ(size.X, size.Y, n, 3, 1)
	palette := make([]color.RGBA, bz.NStates())
	greyInterval := float64(255) / float64(len(palette)-1)
	for i := range palette {
		palette[i] = grey(uint8(math.Round(greyInterval * float64(i))))
	}
	fill(bz, width)

	for i := 0; ; i++ {
		img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
		plotCells(img, bz, palette)
		imgc <- img
		bz.Step()
	}
}

var fillers = map[string]func(bz *BZ, width int){
	"rand":    fillRand,
	"spiral1": spiral,
	"spiral2": doubleSpiral,
	"spiral3": trebleSpiral,
	"spiral4": quadSpiral,
}

func fillRand(bz *BZ, width int) {
	nstates := bz.NStates()
	for j := 0; j < bz.ysize; j += width * 2 {
		for i := 0; i < bz.xsize; i += width * 2 {
			fillRect(bz, image.Rect(i, j, i+width*2, j*width*2), uint8(rand.Intn(nstates)))
		}
	}
}

func spiral(bz *BZ, width int) {
	b := bz.Bounds()
	horizRange(bz, image.Rect(0, b.Max.Y/2, b.Max.X/2, b.Max.Y), true, width)
}

func doubleSpiral(bz *BZ, width int) {
	b := bz.Bounds()
	horizRange(bz, image.Rect(0, b.Max.Y/2, b.Max.X/2, b.Max.Y), true, width)
	horizRange(bz, image.Rect(b.Max.X/2, 0, b.Max.X, b.Max.Y/2+width), false, width)
}

func trebleSpiral(bz *BZ, width int) {
	b := bz.Bounds()
	horizRange(bz, image.Rect(0, b.Max.Y/2, b.Max.X/2, b.Max.Y), true, width)
	horizRange(bz, image.Rect(b.Max.X/2, 0, b.Max.X, b.Max.Y/2+width), false, width)
	vertRange(bz, image.Rect(b.Max.X/2, b.Max.Y/2+width, b.Max.X, b.Max.Y), true, width)
}

func quadSpiral(bz *BZ, width int) {
	b := bz.Bounds()
	horizRange(bz, image.Rect(0, b.Max.Y/2, b.Max.X/2, b.Max.Y), true, width)
	horizRange(bz, image.Rect(b.Max.X/2, 0, b.Max.X, b.Max.Y/2+width), false, width)
	vertRange(bz, image.Rect(b.Max.X/2, b.Max.Y/2+width, b.Max.X, b.Max.Y), true, width)
	vertRange(bz, image.Rect(0, 0, b.Max.X/2, b.Max.Y/2), false, width)
}

func vertRange(bz *BZ, r image.Rectangle, up bool, width int) {
	var incr, x int
	if up {
		x = r.Min.X
		incr = width
	} else {
		x = r.Max.X - width
		incr = -width
	}
	for s := bz.NStates() - 1; s >= 0; s-- {
		fillRect(bz, image.Rect(x, r.Min.Y, x+width, r.Max.Y), uint8(s))
		x += incr
		s--
	}
}

func horizRange(bz *BZ, r image.Rectangle, up bool, width int) {
	var incr, y int
	if up {
		y = r.Min.Y
		incr = width
	} else {
		y = r.Max.Y - width
		incr = -width
	}
	for s := bz.NStates() - 1; s >= 0; s-- {
		fillRect(bz, image.Rect(r.Min.X, y, r.Max.X, y+width), uint8(s))
		y += incr
		s--
	}
}

func fillRect(bz *BZ, r image.Rectangle, s uint8) {
	r = r.Intersect(bz.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			bz.cells[x+y*bz.xsize].s = s
		}
	}
}

func grey(level uint8) color.RGBA {
	return color.RGBA{level, level, level, 255}
}

func plotCells(img *image.RGBA, bz *BZ, palette []color.RGBA) {
	cells := bz.cells
	for i := range cells {
		col := palette[cells[i].s]
		off := i * 4
		pix := img.Pix[off : off+4]
		pix[0] = col.R
		pix[1] = col.G
		pix[2] = col.B
		pix[3] = col.A
	}
}

func f32Point(p image.Point) f32.Point {
	return f32.Point{X: float32(p.X), Y: float32(p.Y)}
}

func f32Rect(r image.Rectangle) f32.Rectangle {
	return f32.Rectangle{Min: f32Point(r.Min), Max: f32Point(r.Max)}
}
