package main

import "github.com/egoist/mygo/ui"

// pal is the colors of one appearance. Light is the design's, value for
// value; dark keeps its structure on a dark ground.
type pal struct {
	dark bool

	ink, nav, muted, soft, body ui.Color // text, from strongest to quietest
	blue, orange, green, danger ui.Color

	sheetTint, sheetEdge, listBg, listEdge, readerBg, readerEdge, card, cardHi, field, fieldEdge ui.Color
	selCard, hover, avatar, hair                                                                 ui.Color
	shadow, shadowSoft, sheetShadow                                                              ui.Color

	base       ui.Color // the window's ground where the system draws no material
	scrim, pop ui.Color // behind a dialog, and the dialog's tint over its blur
}

func palFor(dark bool) pal {
	w := func(a float32) ui.Color { return ui.RGBA(255, 255, 255, a) }
	if !dark {
		return pal{
			ink: ui.Hex("#141821"), nav: ui.Hex("#2B3443"), muted: ui.Hex("#4A5362"), soft: ui.Hex("#3A4352"), body: ui.Hex("#232B38"),
			blue: ui.Hex("#2558D9"), orange: ui.Hex("#C96A10"), green: ui.Hex("#1E8E4E"), danger: ui.Hex("#B3261E"),
			sheetTint: w(.34), sheetEdge: w(.65), listBg: w(.5), listEdge: w(.7), readerBg: w(.82), readerEdge: w(.9), card: w(.62), cardHi: w(.72), field: w(.6),
			fieldEdge: ui.RGBA(20, 30, 60, .07), selCard: w(.92), hover: w(.4), avatar: w(.8),
			hair:   ui.RGBA(20, 30, 60, .1),
			shadow: ui.RGBA(46, 58, 110, .12), shadowSoft: ui.RGBA(46, 58, 110, .08), sheetShadow: ui.RGBA(46, 58, 110, .28),
			base:  ui.Hex("#E8ECF4"),
			scrim: ui.RGBA(20, 30, 60, .16), pop: w(.55),
		}
	}
	return pal{
		dark: true,
		ink:  ui.Hex("#F2F4F8"), nav: ui.Hex("#DDE2EC"), muted: ui.Hex("#A9B3C5"), soft: ui.Hex("#BAC3D3"), body: ui.Hex("#E4E8F0"),
		blue: ui.Hex("#7FA0FF"), orange: ui.Hex("#F0A05A"), green: ui.Hex("#4CC27E"), danger: ui.Hex("#FF8A80"),
		sheetTint: w(.06), sheetEdge: w(.14), listBg: w(.06), listEdge: w(.12), readerBg: w(.1), readerEdge: w(.16), card: w(.08), cardHi: w(.11), field: w(.09),
		fieldEdge: w(.1), selCard: w(.18), hover: w(.07), avatar: w(.16),
		hair:   w(.12),
		shadow: ui.RGBA(0, 0, 0, .3), shadowSoft: ui.RGBA(0, 0, 0, .22), sheetShadow: ui.RGBA(0, 0, 0, .5),
		base:  ui.Hex("#1A1D29"),
		scrim: ui.RGBA(0, 0, 0, .35), pop: ui.RGBA(36, 40, 56, .62),
	}
}

// panel is a card on the page.
func panel(c *ui.Context, p pal, bg ui.Color) ui.Element {
	return ui.Column(c).Gap(12).Padding(20).Radius(16).Background(bg).Shadow(0, 6, 20, 0, p.shadowSoft)
}

// avatar is a person's initial in a circle, on the blue when chosen.
func avatar(c *ui.Context, p pal, name string, size float32, on bool) {
	bg, fg := p.avatar, p.nav
	if on {
		bg, fg = p.blue, ui.RGB(255, 255, 255)
		if p.dark {
			fg = ui.Hex("#0B1020")
		}
	}
	ui.Box(c).Size(size, size).Radius(size / 2).Background(bg).Center().Children(func() {
		ui.Text(c, initial(name)).FontSize(size * 0.4).FontWeight(600).TextColor(fg)
	})
}
