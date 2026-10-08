package svg

import (
	"math"
	"strconv"

	"github.com/yongjohnlee80/golib/gui"
)

// PATH DATA — a <path>'s d: M L H V C S Q T A Z, absolute (upper case) and relative (lower case),
// a command's letter repeated implicitly by more numbers, as SVG writes them ("M0 0 10 10" is a
// move and a line). Arcs become cubics, at most four each. Every segment counts against the
// drawing's MaxSegments.

// scanner reads numbers, flags and command letters from path data or a number list.
type scanner struct {
	s string
	i int
}

func (s *scanner) done() bool { return s.i >= len(s.s) }

// sep skips white space and one comma.
func (s *scanner) sep() {
	comma := false
	for s.i < len(s.s) {
		switch c := s.s[s.i]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f':
		case c == ',' && !comma:
			comma = true
		default:
			return
		}
		s.i++
	}
}

// number reads one number: an optional sign, digits with at most one point, and an exponent.
// "1.5.5" is two numbers, and "1-2" is two, as path data writes them. false: none here.
func (s *scanner) number() (float32, bool, error) {
	s.sep()
	start, i := s.i, s.i
	if i < len(s.s) && (s.s[i] == '+' || s.s[i] == '-') {
		i++
	}
	digits, point := false, false
	for i < len(s.s) {
		c := s.s[i]
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case c == '.' && !point:
			point = true
		default:
			goto exp
		}
		i++
	}
exp:
	if !digits {
		return 0, false, nil
	}
	if i < len(s.s) && (s.s[i] == 'e' || s.s[i] == 'E') {
		j := i + 1
		if j < len(s.s) && (s.s[j] == '+' || s.s[j] == '-') {
			j++
		}
		if j < len(s.s) && s.s[j] >= '0' && s.s[j] <= '9' {
			for j < len(s.s) && s.s[j] >= '0' && s.s[j] <= '9' {
				j++
			}
			i = j
		}
	}
	f, err := strconv.ParseFloat(s.s[start:i], 64)
	if err != nil {
		return 0, false, ErrInvalid // a number too large for float64
	}
	n, err := finite(f)
	if err != nil {
		return 0, false, err
	}
	s.i = i
	return n, true, nil
}

// flag reads an arc's flag, a single 0 or 1 that needs no separator ("a1 1 0 01 2 3").
func (s *scanner) flag() (bool, bool) {
	s.sep()
	if s.i < len(s.s) && (s.s[s.i] == '0' || s.s[s.i] == '1') {
		f := s.s[s.i] == '1'
		s.i++
		return f, true
	}
	return false, false
}

// command reads a command letter, or false when the next thing is a number (an implicit repeat).
func (s *scanner) command() (byte, bool) {
	s.sep()
	if s.i < len(s.s) {
		c := s.s[s.i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			s.i++
			return c, true
		}
	}
	return 0, false
}

// pathData appends d's segments to path. A malformed command ends the data there: what came
// before is drawn, as a browser draws it.
func (p *parser) pathData(d string, path *gui.Path) error {
	s := &scanner{s: d}
	var cur, start, ctrl gui.Point // ctrl: the last cubic or quadratic control, for S and T
	var prev byte
	var cmd byte
	pts := func(n int) ([]float32, bool, error) {
		out := make([]float32, n)
		for i := range out {
			v, ok, err := s.number()
			if err != nil || !ok {
				return nil, false, err
			}
			out[i] = v
		}
		return out, true, nil
	}
	for {
		if c, ok := s.command(); ok {
			cmd = c
		} else if s.done() || cmd == 0 || cmd == 'Z' || cmd == 'z' {
			return nil
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		at := func(x, y float32) gui.Point {
			if rel {
				return gui.Point{X: cur.X + x, Y: cur.Y + y}
			}
			return gui.Point{X: x, Y: y}
		}
		switch up {
		case 'Z':
			if err := p.count(1); err != nil {
				return err
			}
			path.Close()
			cur = start
			prev = 'Z'
			continue
		case 'M', 'L', 'T':
			a, ok, err := pts(2)
			if err != nil || !ok {
				return err
			}
			to := at(a[0], a[1])
			if err := p.count(1); err != nil {
				return err
			}
			switch up {
			case 'M':
				path.MoveTo(to)
				start = to
				// numbers after a move are lines
				if rel {
					cmd = 'l'
				} else {
					cmd = 'L'
				}
			case 'L':
				path.LineTo(to)
			case 'T':
				c := cur
				if prev == 'Q' || prev == 'T' {
					c = gui.Point{X: 2*cur.X - ctrl.X, Y: 2*cur.Y - ctrl.Y}
				}
				path.QuadTo(c, to)
				ctrl = c
			}
			cur = to
		case 'H', 'V':
			a, ok, err := pts(1)
			if err != nil || !ok {
				return err
			}
			to := cur
			switch {
			case up == 'H' && rel:
				to.X += a[0]
			case up == 'H':
				to.X = a[0]
			case rel:
				to.Y += a[0]
			default:
				to.Y = a[0]
			}
			if err := p.count(1); err != nil {
				return err
			}
			path.LineTo(to)
			cur = to
		case 'C':
			a, ok, err := pts(6)
			if err != nil || !ok {
				return err
			}
			c1, c2, to := at(a[0], a[1]), at(a[2], a[3]), at(a[4], a[5])
			if err := p.count(1); err != nil {
				return err
			}
			path.CubeTo(c1, c2, to)
			ctrl, cur = c2, to
		case 'S':
			a, ok, err := pts(4)
			if err != nil || !ok {
				return err
			}
			c1 := cur
			if prev == 'C' || prev == 'S' {
				c1 = gui.Point{X: 2*cur.X - ctrl.X, Y: 2*cur.Y - ctrl.Y}
			}
			c2, to := at(a[0], a[1]), at(a[2], a[3])
			if err := p.count(1); err != nil {
				return err
			}
			path.CubeTo(c1, c2, to)
			ctrl, cur = c2, to
		case 'Q':
			a, ok, err := pts(4)
			if err != nil || !ok {
				return err
			}
			c, to := at(a[0], a[1]), at(a[2], a[3])
			if err := p.count(1); err != nil {
				return err
			}
			path.QuadTo(c, to)
			ctrl, cur = c, to
		case 'A':
			r, ok, err := pts(3)
			if err != nil || !ok {
				return err
			}
			large, ok1 := s.flag()
			sweep, ok2 := s.flag()
			if !ok1 || !ok2 {
				return nil
			}
			e, ok, err := pts(2)
			if err != nil || !ok {
				return err
			}
			to := at(e[0], e[1])
			n, err := p.arc(path, cur, to, r[0], r[1], r[2], large, sweep)
			if err != nil {
				return err
			}
			_ = n
			cur = to
		default:
			return nil // an unknown command ends the data
		}
		prev = up
	}
}

// arc appends the elliptical arc from from to to as cubics (SVG 1.1 F.6.5), counting them; a zero
// radius is a line, and a zero-length arc nothing.
func (p *parser) arc(path *gui.Path, from, to gui.Point, rx, ry, phiDeg float32, large, sweep bool) (int, error) {
	if from == to {
		return 0, nil
	}
	if rx == 0 || ry == 0 {
		if err := p.count(1); err != nil {
			return 0, err
		}
		path.LineTo(to)
		return 1, nil
	}
	r1, r2 := math.Abs(float64(rx)), math.Abs(float64(ry))
	phi := float64(phiDeg) * math.Pi / 180
	cos, sin := math.Cos(phi), math.Sin(phi)
	dx, dy := float64(from.X-to.X)/2, float64(from.Y-to.Y)/2
	x1 := cos*dx + sin*dy
	y1 := -sin*dx + cos*dy
	// radii too small for the chord grow to fit it
	if lambda := x1*x1/(r1*r1) + y1*y1/(r2*r2); lambda > 1 {
		s := math.Sqrt(lambda)
		r1, r2 = r1*s, r2*s
	}
	num := r1*r1*r2*r2 - r1*r1*y1*y1 - r2*r2*x1*x1
	den := r1*r1*y1*y1 + r2*r2*x1*x1
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cx1, cy1 := co*r1*y1/r2, -co*r2*x1/r1
	cx := cos*cx1 - sin*cy1 + float64(from.X+to.X)/2
	cy := sin*cx1 + cos*cy1 + float64(from.Y+to.Y)/2
	angle := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	t1 := angle(1, 0, (x1-cx1)/r1, (y1-cy1)/r2)
	dt := angle((x1-cx1)/r1, (y1-cy1)/r2, (-x1-cx1)/r1, (-y1-cy1)/r2)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dt) / (math.Pi / 2)))
	n = min(max(n, 1), 4)
	if err := p.count(n); err != nil {
		return 0, err
	}
	step := dt / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	pt := func(t float64) (float64, float64) {
		x, y := r1*math.Cos(t), r2*math.Sin(t)
		return cos*x - sin*y + cx, sin*x + cos*y + cy
	}
	deriv := func(t float64) (float64, float64) {
		x, y := -r1*math.Sin(t), r2*math.Cos(t)
		return cos*x - sin*y, sin*x + cos*y
	}
	t := t1
	for i := 0; i < n; i++ {
		x0, y0 := pt(t)
		dx0, dy0 := deriv(t)
		x3, y3 := pt(t + step)
		dx3, dy3 := deriv(t + step)
		c1 := gui.Point{X: float32(x0 + k*dx0), Y: float32(y0 + k*dy0)}
		c2 := gui.Point{X: float32(x3 - k*dx3), Y: float32(y3 - k*dy3)}
		end := gui.Point{X: float32(x3), Y: float32(y3)}
		if i == n-1 {
			end = to
		}
		path.CubeTo(c1, c2, end)
		t += step
	}
	return n, nil
}
