package picker

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/noamsto/themestate"
	"github.com/noamsto/tmux-remux/internal/tmux"
)

// Theme resolves Catppuccin role colors from tmux options, with hardcoded
// Latte (light) and Mocha (dark) fallbacks when an option is unset. The
// fallbacks mirror nix-config `home/theme/palette.nix`: its Latte accents are
// darkened to clear 4.5:1 on the Latte base, which several upstream Catppuccin
// ones do not, so take them from there rather than from upstream.
type Theme struct {
	flavour  string // "dark" | "light"
	tmuxOpts map[string]string
}

// NewTheme reads $XDG_STATE_HOME/theme-state.json for the user's chosen
// flavour and `tmux show -g` for any `@thm_*` overrides. Both lookups are
// best-effort; failures fall back to Mocha defaults.
func NewTheme() Theme {
	return Theme{
		flavour:  themestate.Detect(),
		tmuxOpts: tmux.GlobalOptions("tmux"),
	}
}

func (t Theme) color(tmuxOpt, darkFallback, lightFallback string) color.Color {
	if v, ok := t.tmuxOpts[tmuxOpt]; ok && v != "" {
		return lipgloss.Color(v)
	}
	if t.flavour == "light" {
		return lipgloss.Color(lightFallback)
	}
	return lipgloss.Color(darkFallback)
}

// Base returns the Catppuccin base (background) color for the current theme.
func (t Theme) Base() color.Color { return t.color("@thm_bg", "#1e1e2e", "#eff1f5") }

// Surface0 returns the Catppuccin surface-0, the cursor row's background.
func (t Theme) Surface0() color.Color { return t.color("@thm_surface_0", "#313244", "#ccd0da") }

// Surface1 returns the Catppuccin surface-1 (subtle border / dim background).
func (t Theme) Surface1() color.Color { return t.color("@thm_surface_1", "#45475a", "#bcc0cc") }

// Text returns the Catppuccin primary text color for the current theme.
func (t Theme) Text() color.Color { return t.color("@thm_fg", "#cdd6f4", "#4c4f69") }

// Subtext returns the dimmed text color used for secondary labels.
func (t Theme) Subtext() color.Color { return t.color("@thm_subtext_0", "#a6adc8", "#6c6f85") }

// Overlay returns the dim overlay color used for de-emphasized rows.
func (t Theme) Overlay() color.Color { return t.color("@thm_overlay_1", "#7f849c", "#8c8fa1") }

// Mauve returns the Catppuccin mauve accent used for session names.
func (t Theme) Mauve() color.Color { return t.color("@thm_mauve", "#cba6f7", "#8839ef") }

// Blue returns the Catppuccin blue accent used for window names + headers.
func (t Theme) Blue() color.Color { return t.color("@thm_blue", "#89b4fa", "#1e66f5") }

// Green returns the Catppuccin green accent used for "on" toggle states.
func (t Theme) Green() color.Color { return t.color("@thm_green", "#a6e3a1", "#358023") }

// Yellow returns the Catppuccin yellow accent used for the close list's
// command column and its second pane-block rail.
func (t Theme) Yellow() color.Color { return t.color("@thm_yellow", "#f9e2af", "#996b00") }

// Red returns the Catppuccin red accent used for warnings and invalid state.
func (t Theme) Red() color.Color { return t.color("@thm_red", "#f38ba8", "#d20f39") }

// Lavender returns the Catppuccin lavender accent used for footer key labels.
func (t Theme) Lavender() color.Color { return t.color("@thm_lavender", "#b4befe", "#5a6ad4") }

// ASCIIGlyphs reports whether the scope glyphs should fall back to geometric
// shapes, for a terminal whose font has no Nerd Font icons. Any value but
// "off" counts — the option exists only to ask for the fallback, so there is
// no spelling of it that should quietly do nothing.
func (t Theme) ASCIIGlyphs() bool {
	v := t.tmuxOpts["@remux_ascii_glyphs"]
	return v != "" && v != "off"
}
