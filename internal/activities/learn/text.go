package learn

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// maxTextWidth keeps lines short enough to read comfortably, even in a wide pane.
const maxTextWidth = 72

var (
	dim     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6b7680", Dark: "#7d8b96"})
	bold    = lipgloss.NewStyle().Bold(true)
	italic  = lipgloss.NewStyle().Italic(true)
	accent  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#b4580f", Dark: "#f0a35e"})
	codeSty = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1f5f8b", Dark: "#7fb8e0"})
	warn    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9a6a00", Dark: "#f2c14e"})
	selSty  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#b4580f", Dark: "#f0a35e"})

	boldRe   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	italicRe = regexp.MustCompile(`(^|[^*])\*([^*]+)\*`)
	codeRe   = regexp.MustCompile("`([^`]+)`")
	listRe   = regexp.MustCompile(`^(\s*)([-*]|\d+\.)\s+(.*)$`)
)

// renderMarkdown draws the small subset of Markdown that cards use:
// paragraphs, bullet and numbered lists, **bold**, *italic*, `code` and
// fenced code blocks. A full Markdown renderer would add many dependencies
// and make output harder to test; cards are short prose.
func renderMarkdown(md string, width int) string {
	width = textWidth(width)
	var out []string
	var para []string
	inFence := false

	flush := func() {
		if len(para) > 0 {
			out = append(out, wrap(inline(strings.Join(para, " ")), width, ""))
			para = nil
		}
	}
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			flush()
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, codeSty.Render("  "+line))
			continue
		}
		switch {
		case trimmed == "":
			flush()
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
		case listRe.MatchString(line):
			flush()
			m := listRe.FindStringSubmatch(line)
			marker := "•"
			if m[2] != "-" && m[2] != "*" {
				marker = m[2]
			}
			out = append(out, wrap(inline(m[3]), width, m[1]+marker+" "))
		default:
			para = append(para, trimmed)
		}
	}
	flush()
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

func inline(s string) string {
	s = codeRe.ReplaceAllStringFunc(s, func(m string) string { return codeSty.Render(codeRe.FindStringSubmatch(m)[1]) })
	s = boldRe.ReplaceAllStringFunc(s, func(m string) string { return bold.Render(boldRe.FindStringSubmatch(m)[1]) })
	s = italicRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := italicRe.FindStringSubmatch(m)
		return sub[1] + italic.Render(sub[2])
	})
	return s
}

// wrap word-wraps s to width, putting prefix on the first line and matching
// indentation on the rest (for list items).
func wrap(s string, width int, prefix string) string {
	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	body := lipgloss.NewStyle().Width(max(width-len(indent), 10)).Render(s)
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		l = strings.TrimRight(l, " ")
		if i == 0 {
			lines[i] = prefix + l
		} else {
			lines[i] = indent + l
		}
	}
	return strings.Join(lines, "\n")
}

func textWidth(width int) int { return max(min(width-2, maxTextWidth), 20) }
