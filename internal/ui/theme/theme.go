// Package theme provides the rp6 application's Fyne theme: the standard dark
// theme with its blue accent replaced by the same amber used on the LCDs.
package theme

import (
	"image/color"

	"fyne.io/fyne/v2"
	ftheme "fyne.io/fyne/v2/theme"
)

// amber is the accent color, matching the LCD readouts.
// amber is the accent color. It matches the "bank B" pad orange (#E1873B),
// which is dark enough that white foreground text stays readable when it is
// used as a button/selection background.
var (
	amber      = color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0xFF}
	amberFocus = color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0x88}
	amberSel   = color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0x66}
	amberHover = color.NRGBA{R: 0xE1, G: 0x87, B: 0x3B, A: 0x33}
)

// Amber is a dark theme whose accent (primary) color is amber/orange instead of
// Fyne's default blue.
type Amber struct{}

var _ fyne.Theme = Amber{}

// Color returns amber for accent-related names and the default dark theme's
// color otherwise. The dark variant is forced regardless of the OS setting.
func (Amber) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case ftheme.ColorNamePrimary, ftheme.ColorNameHyperlink:
		return amber
	case ftheme.ColorNameFocus:
		return amberFocus
	case ftheme.ColorNameSelection:
		return amberSel
	case ftheme.ColorNameHover:
		return amberHover
	}
	return ftheme.DefaultTheme().Color(name, ftheme.VariantDark)
}

func (Amber) Font(s fyne.TextStyle) fyne.Resource     { return ftheme.DefaultTheme().Font(s) }
func (Amber) Icon(n fyne.ThemeIconName) fyne.Resource { return ftheme.DefaultTheme().Icon(n) }
func (Amber) Size(n fyne.ThemeSizeName) float32       { return ftheme.DefaultTheme().Size(n) }
