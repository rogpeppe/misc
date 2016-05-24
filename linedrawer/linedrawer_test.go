package linedrawer

import (
	gc "gopkg.in/check.v1"
)

type suite struct{}

var _ = gc.Suite(&suite{})

type scaleParams struct {
	dstr image.Rectangle
	srcr image.Rectangle
}

var paintTests = []struct {
	dproc        *drawerProc
	expectScale1 *scaleParams
	expectScale2 *scaleParams
}{}

func (*suite) TestPaint(c *gc.C) {
}
