package linedrawer

import (
	"image"
	"image/color"
	"image/draw"
	"log"
	"os"
	"sync"
	"time"

	"gopkg.in/errgo.v1"

	"golang.org/x/exp/shiny/driver"
	"golang.org/x/exp/shiny/screen"
	"golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
)

// LineDrawer is implemented by a display that can
// show successive lines of a 1-dimensional cellular
// automaton.
type LineDrawer interface {
	// DrawLine draws a line of cells. The length of the cells slice
	// will be the value passed to the NewLineDrawer function;
	// each element will contain only values from 0 to numStates-1.
	DrawLine(cells []int)
}

var (
	logMu   sync.Mutex
	logging bool
)

func logf(f string, a ...interface{}) {
	logMu.Lock()
	logging := logging
	logMu.Unlock()
	if logging {
		log.Printf(f, a...)
	}
}

func setLogging(on bool) {
	logMu.Lock()
	logging = on
	logMu.Unlock()
}

const (
	tickDuration = time.Second / 30
)

var colors = []color.RGBA{
	colorBlack.rgba(),
	colorRed.rgba(),
	colorOrchid.rgba(),
	colorOrange.rgba(),
}

type NewFunc func(numCells, numStates int) (LineDrawer, error)

func Main(f func(NewFunc)) {
	driver.Main(func(s screen.Screen) {
		ctxt := context{
			screen: s,
		}
		f(ctxt.new)
	})
}

type drawer struct {
	numCells  int
	numStates int

	paintNotifier paintNotifier
	mu            sync.Mutex
	row0          int // index of first row
	rows          [][]int

	screen screen.Screen
	win    screen.Window

	// size holds the latest received size event.
	size size.Event

	// rowDisplay holds the row that should be displayed at the top
	// of the screen.
	// TODO this should probably be specified in pixels not rows.
	rowDisplay int

	// numRows hows the number of rows that can be displayed on the
	// screen. Each buffer holds this many rows of pixels.
	numRows int

	// scratch is used to store pixels before uploading
	// them to buf0 and buf1. It's the same size as
	// buf0 and buf1.
	scratch screen.Buffer

	// buf0 and buf1 hold the displayed pixels.
	buf0, buf1 screen.Texture

	// bufp0 holds the first buffered row number.
	// This is stored in the top row of buf0.
	bufp0 int

	// bufp1 holds the last buffered row number (the
	// number of rows held in the buffer is bufp0 - bufp1)
	bufp1 int

	// cellWidth holds the width of a cell in pixels.
	cellWidth float32
}

type context struct {
	screen screen.Screen
}

func (ctxt *context) new(numCells, numStates int) (LineDrawer, error) {
	if numStates > len(colors) {
		return nil, errgo.Newf("too many states for available colors")
	}
	w, err := ctxt.screen.NewWindow(nil)
	if err != nil {
		return nil, err
	}
	d := &drawer{
		screen: ctxt.screen,
		win:    w,

		numCells:  numCells,
		numStates: numStates,
	}
	d.paintNotifier.setQueue(w)
	go d.paintNotifier.run()
	go func() {
		d.main()
		os.Exit(0)
		// TODO better than this
	}()
	return d, nil
}

func (d *drawer) DrawLine(cells []int) {
	if len(cells) != d.numCells {
		panic("unexpected cell count")
	}
	cells1 := make([]int, len(cells))
	copy(cells1, cells)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rows = append(d.rows, cells1)
	d.paintNotifier.changed()
}

type paintNotifier struct {
	mu                sync.Mutex
	q                 screen.EventQueue
	generation        int
	paintedGeneration int
}

func (p *paintNotifier) run() {
	ticker := time.NewTicker(tickDuration)
	for range ticker.C {
		p.tick()
	}
}

func (p *paintNotifier) setQueue(q screen.EventQueue) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.q = q
}

func (p *paintNotifier) tick() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.paintedGeneration != p.generation && p.q != nil {
		p.q.Send(paint.Event{})
	}
}

func (p *paintNotifier) changed() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generation++
}

func (p *paintNotifier) painted() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paintedGeneration = p.generation
}

func (d *drawer) main() {
	paused := false
	logging := false
	for {
		e := d.win.NextEvent()
		//	logf("got event %T (%#v)", e, e)
		switch e := e.(type) {
		case lifecycle.Event:
			if e.To == lifecycle.StageDead {
				return
			}

		case key.Event:
			if e.Code == key.CodeEscape {
				return
			}
			if e.Code == key.CodeSpacebar {
				paused = !paused
			}
			if e.Code == key.CodeL {
				logging = !logging
				setLogging(logging)
			}

		case paint.Event:
			if paused {
				break
			}
			d.paint()
			d.win.Publish()
			d.paintNotifier.painted()

		case size.Event:
			// Fit all the cells exactly across the screen using square cells.
			if err := d.setSize(e); err != nil {
				log.Printf("cannot set size: %v", err)
			}

		case error:
			log.Printf("error event: %v", e)
		}
	}
}

type releaser interface {
	Release()
}

func (d *drawer) setSize(e size.Event) (err error) {
	cellWidth := float32(e.WidthPx) / float32(d.numCells)
	releaseOnError := func(r releaser) {
		if err != nil {
			r.Release()
		}
	}

	numRows := int(float32(e.HeightPx)/cellWidth + 1) // Plus one for luck.
	buf0, err := d.screen.NewTexture(image.Point{d.numCells, numRows})
	if err != nil {
		return errgo.Notef(err, "cannot allocate texture 0")
	}
	defer releaseOnError(buf0)

	buf1, err := d.screen.NewTexture(image.Point{d.numCells, numRows})
	if err != nil {
		return errgo.Notef(err, "cannot allocate texture 1")
	}
	defer releaseOnError(buf1)

	scratch, err := d.screen.NewBuffer(image.Point{d.numCells, numRows})
	if err != nil {
		return errgo.Notef(err, "cannot allocate buffer")
	}
	d.size = e
	d.cellWidth = cellWidth
	d.buf0 = buf0
	d.buf1 = buf1
	d.numRows = numRows
	d.scratch = scratch
	d.bufp0 = 0
	d.bufp1 = 0
	logf("size set to %v", image.Pt(d.size.WidthPx, d.size.HeightPx))
	logf("numRows %d; cellWidth %g", d.numRows, d.cellWidth)
	return nil
}

func (d *drawer) paint() {
	if d.size.WidthPx == 0 || d.size.HeightPx == 0 {
		logf("draw with no size set")
		return
	}
	d.fillBuffers()
	logf("after fillBuffers, bufp %d %d", d.bufp0, d.bufp1)

	if d.bufp1 <= d.rowDisplay {
		// No rows to display.
		return
	}
	// We know that at least the first buffer has some lines to display
	// and that d.rowDisplay > d.bufp0 and d.rowDisplay < d.bufp1

	// boundary holds the upper bound of the row
	// to be displayed from buf0.
	boundary := min(d.bufp1, d.bufp0+d.numRows)

	numRows0 := boundary - d.rowDisplay
	r0 := image.Rect(
		0, 0,
		d.size.WidthPx, int(d.cellWidth*float32(numRows0)),
	)
	logf("r0 %v from %v", r0, image.Rect(
		0, d.rowDisplay-d.bufp0,
		d.numCells, min(d.bufp1, d.bufp0+d.numRows)-d.bufp0,
	))
	d.win.Scale(
		r0,
		d.buf0,
		image.Rect(
			0, d.rowDisplay-d.bufp0,
			d.numCells, min(d.bufp1, d.bufp0+d.numRows)-d.bufp0,
		),
		draw.Over,
		nil,
	)
	end := min(d.rowDisplay+d.numRows, d.bufp1)
	logf("end %d (min %d %d)", end, d.rowDisplay+d.numRows, d.bufp1)
	logf("boundary %d", boundary)
	if end <= boundary {
		// Nothing to be displayed from buf1.
		return
	}
	numRows1 := end - (d.bufp0 + d.numRows)
	logf("numRows1 %d", numRows1)
	r1 := image.Rect(
		0, r0.Max.Y,
		d.size.WidthPx, d.size.HeightPx,
	)
	logf("r1 %v from %v", r1, image.Rect(
		0, 0,
		d.numCells, numRows1,
	))
	d.win.Scale(
		r1,
		d.buf1,
		image.Rect(
			0, 0,
			d.numCells, numRows1,
		),
		draw.Over,
		nil,
	)
}

func (d *drawer) fillBuffers() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.rows) > d.numRows {
		extra := len(d.rows) - d.numRows
		d.rows = d.rows[extra:]
		d.row0 += extra
	}
	d.rowDisplay = d.row0
	row1 := d.row0 + len(d.rows)
	logf("filling buffers; rows %d %d; bufp %d %d; display rows %d", d.row0, row1, d.bufp0, d.bufp1, d.numRows)

	// [dp0, dp1] holds the set of rows we need to display.
	dp0, dp1 := d.rowDisplay, d.rowDisplay+d.numRows
	if dp1 > row1 {
		dp1 = row1
	}
	logf("dp %d %d", dp0, dp1)
	// We need to draw [dp0, dp1]. We have [d.bufp0, d.bufp1]
	// cached. Find whether we can use any of the cached rows.
	usep0, usep1 := dp0, dp1
	if usep0 < d.bufp0 {
		usep0 = d.bufp0
	}
	if usep1 > d.bufp1 {
		usep1 = d.bufp1
	}
	logf("use: %d %d", usep0, usep1)
	var fillp0, fillp1 int
	if usep0 < usep1 {
		// We can use [usep0, usep1]
		if usep0-d.bufp0 > d.numRows {
			// The cached area we can use starts
			// after the first buffer. Swap the buffers
			// so that we know we've got enough space
			// to draw all the rows we need.
			d.buf0, d.buf1 = d.buf1, d.buf0
			d.buf1.Fill(d.buf1.Bounds(), colorGreen, draw.Src)
			d.bufp0 += d.numRows
		}
		fillp0, fillp1 = usep1, dp1
	} else {
		// No buffered rows. Start filling from the start of buf0.
		fillp0, fillp1 = dp0, dp1
		d.bufp0 = d.rowDisplay
		d.bufp1 = d.rowDisplay
	}
	logf("fillp %d %d", fillp0, fillp1)
	logf("bufp %d %d", d.bufp0, d.bufp1)

	// Fill the scratch buffer with all the rows that we need to draw.
	rgba := d.scratch.RGBA()

	//	draw.Op.Draw(rgba,

	for row := fillp0; row < fillp1; row++ {
		//		logf("fill row %d (offset %d) %v", row, row-fillp0, d.rows[row - d.row0])
		off := rgba.PixOffset(0, row-fillp0)
		pix := rgba.Pix[off : off+4*d.numCells]
		d.fillRow(pix, d.rows[row-d.row0])
	}
	// boundary holds the offset of the first row in buf1.
	boundary := d.bufp0 + d.numRows
	copied := 0
	if fillp0 < boundary {
		r1 := min(fillp1, boundary)
		// Upload the first batch of rows.
		// b0 and b1 hold the destination y range in buf0.
		b0, b1 := fillp0-d.bufp0, r1-d.bufp0
		logf("upload 0 to %d from %v", b0, image.Rect(0, 0, d.numCells, b1-b0))
		d.buf0.Upload(image.Pt(0, b0), d.scratch, image.Rect(0, 0, d.numCells, b1-b0))
		copied = b1 - b0
	}

	// r0 holds the offset of the first row in buf1.
	if fillp1 > boundary {
		// There are rows to be uploaded into tex2: [r0, fillp1]

		// b0 and b1 hold the destination y range in buf1.
		b0 := fillp0 - boundary
		if b0 < 0 {
			b0 = 0
		}
		b1 := fillp1 - boundary
		logf("upload 1 to %d from %v", b0, image.Rect(0, copied, d.numCells, b1-b0+copied))
		d.buf1.Upload(image.Pt(0, b0), d.scratch, image.Rect(0, copied, d.numCells, b1-b0+copied))
	}
	d.bufp1 = dp1
}

func (d *drawer) fillRow(pix []byte, cells []int) {
	for i, c := range cells {
		if c < 0 || c >= d.numStates {
			panic("cell value out of range")
		}
		rgb := colors[c]
		pix1 := pix[i*4:]
		pix1[0] = rgb.R
		pix1[1] = rgb.G
		pix1[2] = rgb.B
		pix1[3] = rgb.A
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
