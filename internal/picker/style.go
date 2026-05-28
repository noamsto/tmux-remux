package picker

import "charm.land/lipgloss/v2"

// Catppuccin Mocha (matches lazytmux's picker for visual continuity).
var (
	colBase     = lipgloss.Color("#1e1e2e")
	colSurface0 = lipgloss.Color("#313244")
	colSurface1 = lipgloss.Color("#45475a")
	colText     = lipgloss.Color("#cdd6f4")
	colSubtext  = lipgloss.Color("#a6adc8")
	colOverlay  = lipgloss.Color("#7f849c")
	colMauve    = lipgloss.Color("#cba6f7")
	colBlue     = lipgloss.Color("#89b4fa")
	colGreen    = lipgloss.Color("#a6e3a2")
	colYellow   = lipgloss.Color("#f9e2af")
	colRed      = lipgloss.Color("#f38ba8")
)

var (
	listFrame    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colSurface1).Padding(0, 1)
	treeFrame    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colSurface1).Padding(0, 1)
	previewFrame = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colSurface1).Padding(0, 1)

	rowActive  = lipgloss.NewStyle().Foreground(colBase).Background(colMauve).Bold(true)
	rowDefault = lipgloss.NewStyle().Foreground(colText)
	rowDim     = lipgloss.NewStyle().Foreground(colOverlay)

	nodeKept    = lipgloss.NewStyle().Foreground(colText)
	nodeSkipped = lipgloss.NewStyle().Foreground(colOverlay).Strikethrough(true)
	skipReason  = lipgloss.NewStyle().Foreground(colSubtext).Italic(true)

	footerBar  = lipgloss.NewStyle().Foreground(colSubtext).Padding(0, 1)
	footerWarn = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	footerOn   = lipgloss.NewStyle().Foreground(colGreen)
	footerOff  = lipgloss.NewStyle().Foreground(colOverlay)
)
