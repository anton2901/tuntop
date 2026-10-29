// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import "github.com/charmbracelet/lipgloss"

// Theme is a color scheme. Colors are lipgloss.TerminalColor so a theme can
// use fixed colors (degraded automatically to 256/16 colors) or adaptive
// ones that follow the terminal background.
type Theme struct {
	Name string
	// Fg is ordinary text, Dim secondary text, Title box titles.
	Fg, Dim, Title lipgloss.TerminalColor
	// Hi marks hotkeys (btop's red letters).
	Hi lipgloss.TerminalColor
	// SelFg/SelBg draw the row under the cursor.
	SelFg, SelBg  lipgloss.TerminalColor
	OK, Warn, Err lipgloss.TerminalColor
	// Box colors of the three panels.
	BoxStatus, BoxTraffic, BoxMain lipgloss.TerminalColor
	// Graph gradients, low to high.
	Down, Up [3]lipgloss.TerminalColor
	// Logo gradient, top to bottom.
	Logo [5]lipgloss.TerminalColor
}

func hex(s string) lipgloss.Color { return lipgloss.Color(s) }

func ad(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}

// Themes are the built-in schemes, in menu order. The first is the default.
var Themes = []Theme{
	{
		// Close to btop's default theme.
		Name: "btop",
		Fg:   hex("#cccccc"), Dim: hex("#8a8a8a"), Title: hex("#eeeeee"), Hi: hex("#d45454"),
		SelFg: hex("#eeeeee"), SelBg: hex("#6a2f2f"),
		OK: hex("#5fb85f"), Warn: hex("#d7b740"), Err: hex("#e05a5a"),
		BoxStatus: hex("#3d7b46"), BoxTraffic: hex("#5a4f9a"), BoxMain: hex("#923535"),
		Down: [3]lipgloss.TerminalColor{hex("#4a7ab8"), hex("#7c5cc4"), hex("#c45cb0")},
		Up:   [3]lipgloss.TerminalColor{hex("#3d8a5a"), hex("#a8a43a"), hex("#c45a3a")},
		Logo: [5]lipgloss.TerminalColor{hex("#ff5c5c"), hex("#e84848"), hex("#cc3a3a"), hex("#a82e2e"), hex("#842424")},
	},
	{
		// Follows the terminal: light or dark variant by background.
		Name: "auto",
		Fg:   lipgloss.NoColor{}, Dim: ad("243", "250"), Title: lipgloss.NoColor{}, Hi: ad("160", "203"),
		SelFg: ad("255", "232"), SelBg: ad("25", "111"),
		OK: ad("28", "114"), Warn: ad("130", "221"), Err: ad("160", "210"),
		BoxStatus: ad("28", "71"), BoxTraffic: ad("61", "104"), BoxMain: ad("124", "167"),
		Down: [3]lipgloss.TerminalColor{ad("25", "75"), ad("61", "141"), ad("125", "176")},
		Up:   [3]lipgloss.TerminalColor{ad("28", "78"), ad("136", "185"), ad("130", "209")},
		Logo: [5]lipgloss.TerminalColor{ad("160", "203"), ad("160", "203"), ad("124", "167"), ad("124", "167"), ad("88", "131")},
	},
	{
		Name: "nord",
		Fg:   hex("#d8dee9"), Dim: hex("#81a1c1"), Title: hex("#eceff4"), Hi: hex("#bf616a"),
		SelFg: hex("#2e3440"), SelBg: hex("#88c0d0"),
		OK: hex("#a3be8c"), Warn: hex("#ebcb8b"), Err: hex("#bf616a"),
		BoxStatus: hex("#a3be8c"), BoxTraffic: hex("#b48ead"), BoxMain: hex("#5e81ac"),
		Down: [3]lipgloss.TerminalColor{hex("#5e81ac"), hex("#81a1c1"), hex("#88c0d0")},
		Up:   [3]lipgloss.TerminalColor{hex("#a3be8c"), hex("#ebcb8b"), hex("#d08770")},
		Logo: [5]lipgloss.TerminalColor{hex("#88c0d0"), hex("#81a1c1"), hex("#81a1c1"), hex("#5e81ac"), hex("#5e81ac")},
	},
	{
		Name: "gruvbox",
		Fg:   hex("#ebdbb2"), Dim: hex("#a89984"), Title: hex("#fbf1c7"), Hi: hex("#fb4934"),
		SelFg: hex("#282828"), SelBg: hex("#d79921"),
		OK: hex("#b8bb26"), Warn: hex("#fabd2f"), Err: hex("#fb4934"),
		BoxStatus: hex("#98971a"), BoxTraffic: hex("#b16286"), BoxMain: hex("#d65d0e"),
		Down: [3]lipgloss.TerminalColor{hex("#458588"), hex("#83a598"), hex("#8ec07c")},
		Up:   [3]lipgloss.TerminalColor{hex("#98971a"), hex("#d79921"), hex("#d65d0e")},
		Logo: [5]lipgloss.TerminalColor{hex("#fabd2f"), hex("#fe8019"), hex("#fe8019"), hex("#d65d0e"), hex("#af3a03")},
	},
	{
		// Only the 16 terminal colors: the terminal's own palette decides.
		Name: "tty",
		Fg:   lipgloss.NoColor{}, Dim: lipgloss.Color("7"), Title: lipgloss.NoColor{}, Hi: lipgloss.Color("9"),
		SelFg: lipgloss.Color("0"), SelBg: lipgloss.Color("6"),
		OK: lipgloss.Color("2"), Warn: lipgloss.Color("3"), Err: lipgloss.Color("1"),
		BoxStatus: lipgloss.Color("2"), BoxTraffic: lipgloss.Color("5"), BoxMain: lipgloss.Color("1"),
		Down: [3]lipgloss.TerminalColor{lipgloss.Color("4"), lipgloss.Color("5"), lipgloss.Color("13")},
		Up:   [3]lipgloss.TerminalColor{lipgloss.Color("2"), lipgloss.Color("3"), lipgloss.Color("1")},
		Logo: [5]lipgloss.TerminalColor{lipgloss.Color("9"), lipgloss.Color("9"), lipgloss.Color("1"), lipgloss.Color("1"), lipgloss.Color("1")},
	},
}

// themeByName returns the named theme, or the default.
func themeByName(name string) Theme {
	for _, t := range Themes {
		if t.Name == name {
			return t
		}
	}
	return Themes[0]
}

// Style shortcuts bound to the current theme.
func (t Theme) fg(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
func (t Theme) text() lipgloss.Style                       { return t.fg(t.Fg) }
func (t Theme) dim() lipgloss.Style                        { return t.fg(t.Dim) }
func (t Theme) title() lipgloss.Style                      { return t.fg(t.Title).Bold(true) }
func (t Theme) hi() lipgloss.Style                         { return t.fg(t.Hi).Bold(true) }
func (t Theme) ok() lipgloss.Style                         { return t.fg(t.OK) }
func (t Theme) warn() lipgloss.Style                       { return t.fg(t.Warn) }
func (t Theme) err() lipgloss.Style                        { return t.fg(t.Err) }
func (t Theme) sel() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.SelFg).Background(t.SelBg).Bold(true)
}
