package effects

import (
	"image"
	"image/color"
)

// Icon returns a small monochrome (black) icon representing the effect kind,
// suitable for a pad badge, or nil for KindNone. It uses only the standard
// library so the package stays UI-framework-agnostic.
func (k Kind) Icon() image.Image {
	switch k {
	case KindRoll:
		return rollIcon()
	default:
		return nil
	}
}

// rollIcon draws a "fast-forward" pair of right-pointing triangles.
func rollIcon() image.Image {
	const s = 32
	const ss = 3 // supersampling for smooth edges
	img := image.NewNRGBA(image.Rect(0, 0, s, s))

	tris := [2][6]float64{
		{0.08 * s, 0.20 * s, 0.08 * s, 0.80 * s, 0.46 * s, 0.50 * s},
		{0.50 * s, 0.20 * s, 0.50 * s, 0.80 * s, 0.88 * s, 0.50 * s},
	}
	for y := range s {
		for x := range s {
			cov := 0
			for sy := range ss {
				for sx := range ss {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					for _, t := range tris {
						if inTriangle(px, py, t[0], t[1], t[2], t[3], t[4], t[5]) {
							cov++
							break
						}
					}
				}
			}
			if cov > 0 {
				img.SetNRGBA(x, y, color.NRGBA{A: uint8(0xFF * cov / (ss * ss))})
			}
		}
	}
	return img
}

func inTriangle(px, py, ax, ay, bx, by, cx, cy float64) bool {
	d1 := edgeSign(px, py, ax, ay, bx, by)
	d2 := edgeSign(px, py, bx, by, cx, cy)
	d3 := edgeSign(px, py, cx, cy, ax, ay)
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNeg && hasPos)
}

func edgeSign(px, py, ax, ay, bx, by float64) float64 {
	return (px-bx)*(ay-by) - (ax-bx)*(py-by)
}
