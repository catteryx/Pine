package main

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// renderMarkdown renders a useful subset of Markdown with native widgets:
// headings, paragraphs with inline styling, lists, blockquotes, fenced code
// blocks with syntax highlighting, and horizontal rules. One Selectable
// container wraps it all, so a selection spans its paragraphs.
func renderMarkdown(c *ui.Context, t *ui.Theme, src string) {
	ui.Column(c).Selectable().Gap(10).Children(func() {
		renderMarkdownBody(c, t, src)
	})
}

func renderMarkdownBody(c *ui.Context, t *ui.Theme, src string) {
	lines := strings.Split(src, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			i++
			continue
		}
		if fence := fenceMarker(trimmed); fence != "" {
			lang := strings.TrimSpace(trimmed[len(fence):])
			i++
			var code []string
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			renderCodeBlock(c, t, lang, strings.Join(code, "\n"))
			continue
		}
		if level, text, ok := headingLine(trimmed); ok {
			renderHeading(c, t, level, text)
			i++
			continue
		}
		if i+1 < len(lines) && isPlainLine(trimmed) {
			if level, ok := setextUnderline(lines[i+1]); ok {
				renderHeading(c, t, level, trimmed)
				i += 2
				continue
			}
		}
		if ruleLine(trimmed) {
			ui.Divider(c)
			i++
			continue
		}
		if header, rows, next, ok := tableAt(lines, i); ok {
			renderTable(c, t, header, rows)
			i = next
			continue
		}
		if strings.HasPrefix(trimmed, ">") {
			var quote []string
			for i < len(lines) {
				q := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(q, ">") {
					break
				}
				quote = append(quote, strings.TrimSpace(strings.TrimPrefix(q, ">")))
				i++
			}
			renderQuote(c, t, strings.Join(quote, " "))
			continue
		}
		if _, _, _, ok := listItemLine(line); ok {
			i = renderList(c, t, lines, i)
			continue
		}
		var para []string
		for i < len(lines) {
			l := lines[i]
			tr := strings.TrimSpace(l)
			if tr == "" || fenceMarker(tr) != "" || strings.HasPrefix(tr, ">") || ruleLine(tr) {
				break
			}
			if _, _, ok := headingLine(tr); ok {
				break
			}
			if _, _, _, ok := listItemLine(l); ok {
				break
			}
			if len(para) > 0 {
				if _, _, _, ok := tableAt(lines, i); ok {
					break
				}
				if _, ok := setextUnderline(lines[i]); ok {
					break
				}
			}
			para = append(para, l)
			i++
		}
		renderParagraph(c, t, joinParagraph(para))
	}
}

// isPlainLine reports whether a line can be a paragraph or setext heading
// text.
func isPlainLine(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	if fenceMarker(trimmed) != "" || strings.HasPrefix(trimmed, ">") || ruleLine(trimmed) {
		return false
	}
	if _, _, ok := headingLine(trimmed); ok {
		return false
	}
	if _, _, _, ok := listItemLine(trimmed); ok {
		return false
	}
	return true
}

// setextUnderline reports the heading level an underline line sets: 1 for
// "===", 2 for "---".
func setextUnderline(line string) (int, bool) {
	s := strings.TrimSpace(line)
	if s == "" {
		return 0, false
	}
	ch := s[0]
	if ch != '=' && ch != '-' {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != ch && s[i] != ' ' {
			return 0, false
		}
	}
	if ch == '=' {
		return 1, true
	}
	return 2, true
}

// joinParagraph joins wrapped source lines, honoring hard breaks (two
// trailing spaces or a trailing backslash).
func joinParagraph(raw []string) string {
	var b strings.Builder
	for idx, l := range raw {
		hard := strings.HasSuffix(l, "  ") || strings.HasSuffix(l, "\\")
		l = strings.TrimRight(l, "\\")
		b.WriteString(strings.TrimSpace(l))
		if idx < len(raw)-1 {
			if hard {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		}
	}
	return b.String()
}

// --- tables ----------------------------------------------------------------

func tableAt(lines []string, i int) (header []string, rows [][]string, next int, ok bool) {
	if i+1 >= len(lines) {
		return nil, nil, 0, false
	}
	header = splitRow(lines[i])
	if header == nil {
		return nil, nil, 0, false
	}
	delim := splitRow(lines[i+1])
	if delim == nil || len(delim) != len(header) {
		return nil, nil, 0, false
	}
	for _, cell := range delim {
		if !delimCell(cell) {
			return nil, nil, 0, false
		}
	}
	j := i + 2
	for j < len(lines) {
		row := splitRow(lines[j])
		if row == nil {
			break
		}
		rows = append(rows, row)
		j++
	}
	return header, rows, j, true
}

func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	if !strings.Contains(s, "|") {
		return nil
	}
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func delimCell(cell string) bool {
	cell = strings.TrimSpace(cell)
	cell = strings.Trim(cell, ":")
	if cell == "" {
		return false
	}
	for i := 0; i < len(cell); i++ {
		if cell[i] != '-' {
			return false
		}
	}
	return true
}

func renderTable(c *ui.Context, t *ui.Theme, header []string, rows [][]string) {
	cols := len(header)
	ui.Box(c).Border(1, t.Border).Radius(8).Clip().Margin(4, 0).Children(func() {
		renderTableRow(c, t, header, cols, true)
		for _, row := range rows {
			renderTableRow(c, t, row, cols, false)
		}
	})
}

func renderTableRow(c *ui.Context, t *ui.Theme, cells []string, cols int, head bool) {
	ui.Row(c).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		for k := 0; k < cols; k++ {
			cell := ""
			if k < len(cells) {
				cell = cells[k]
			}
			box := ui.Box(c).Grow(1).Padding(6, 10)
			if k < cols-1 {
				box.BorderWidth(0, 1, 0, 0).BorderColor(t.Border)
			}
			if head {
				box.Background(t.Surface)
			}
			box.Children(func() {
				el := renderInline(c, t, cell)
				if head {
					el.Bold()
				}
			})
		}
	})
}

func fenceMarker(s string) string {
	switch {
	case strings.HasPrefix(s, "```"):
		return "```"
	case strings.HasPrefix(s, "~~~"):
		return "~~~"
	}
	return ""
}

func headingLine(s string) (int, string, bool) {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(s) || s[n] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(s[n:]), true
}

func ruleLine(s string) bool {
	if len(s) < 3 {
		return false
	}
	ch := s[0]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	count := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ch:
			count++
		case ' ':
		default:
			return false
		}
	}
	return count >= 3
}

// listItemLine parses a list item, returning its content, nesting depth and
// whether it is ordered.
func listItemLine(line string) (content string, indent int, ordered bool, ok bool) {
	j := 0
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	rest := line[j:]
	if len(rest) >= 2 && (rest[0] == '-' || rest[0] == '*' || rest[0] == '+') && rest[1] == ' ' {
		return strings.TrimSpace(rest[2:]), j / 2, false, true
	}
	k := 0
	for k < len(rest) && rest[k] >= '0' && rest[k] <= '9' {
		k++
	}
	if k > 0 && k+1 < len(rest) && rest[k] == '.' && rest[k+1] == ' ' {
		return strings.TrimSpace(rest[k+2:]), j / 2, true, true
	}
	return "", 0, false, false
}

func renderHeading(c *ui.Context, t *ui.Theme, level int, text string) {
	sizes := map[int]float32{1: 23, 2: 20, 3: 17, 4: 15, 5: 14, 6: 13}
	size := sizes[level]
	renderInline(c, t, text).FontSize(size).Bold().Margin(6, 0, 2, 0)
}

func renderParagraph(c *ui.Context, t *ui.Theme, text string) {
	renderInline(c, t, text).LineHeight(1.55).Margin(1, 0)
}

func renderQuote(c *ui.Context, t *ui.Theme, text string) {
	ui.Row(c).Gap(10).Margin(2, 0).Children(func() {
		ui.Box(c).Width(3).Background(t.Accent).Radius(2)
		ui.Box(c).Grow(1).Children(func() {
			renderInline(c, t, text).TextColor(t.TextMuted).Italic().LineHeight(1.5)
		})
	})
}

func renderList(c *ui.Context, t *ui.Theme, lines []string, i int) int {
	ui.Column(c).Gap(3).Margin(2, 0).Children(func() {
		num := 1
		for i < len(lines) {
			content, indent, ordered, ok := listItemLine(lines[i])
			if !ok {
				break
			}
			marker, color := bulletMarker(indent), t.TextMuted
			if ordered {
				marker = fmt.Sprintf("%d.", num)
				num++
			}
			if checked, isTask, rest := parseTask(content); isTask {
				content = rest
				color = t.Accent
				if checked {
					marker = "☑"
				} else {
					marker = "☐"
				}
			}
			ui.Row(c).Gap(8).AlignItems(ui.Start).Padding(0, 0, 0, float32(indent)*14).Children(func() {
				ui.Text(c, marker).TextColor(color).FontSize(13).Width(18).TextAlign(ui.End)
				in := renderInline(c, t, content).Grow(1).LineHeight(1.5)
				if strings.HasPrefix(marker, "☑") {
					in.TextColor(t.TextMuted).Strikethrough()
				}
			})
			i++
		}
	})
	return i
}

func bulletMarker(indent int) string {
	switch indent % 3 {
	case 0:
		return "•"
	case 1:
		return "◦"
	default:
		return "▪"
	}
}

// parseTask reads a GFM task-list marker like "[x] ".
func parseTask(content string) (checked, ok bool, rest string) {
	switch {
	case content == "[ ]":
		return false, true, ""
	case content == "[x]", content == "[X]":
		return true, true, ""
	case strings.HasPrefix(content, "[ ] "):
		return false, true, content[4:]
	case strings.HasPrefix(content, "[x] "), strings.HasPrefix(content, "[X] "):
		return true, true, content[4:]
	}
	return false, false, content
}

func renderCodeBlock(c *ui.Context, t *ui.Theme, lang, code string) {
	code = strings.TrimRight(code, "\n")
	ui.Box(c).Background(codeBg(t)).Border(1, t.Border).Radius(10).Clip().Margin(4, 0).Children(func() {
		ui.Row(c).Padding(6, 10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			label := lang
			if label == "" {
				label = "code"
			}
			ui.Text(c, strings.ToLower(label)).FontSize(11).TextColor(t.TextMuted).Grow(1)
			cp := ui.ButtonBase(c).Padding(2, 6).Radius(6).Shrink(0).
				Cursor(ui.CursorPointer).OnClick(func() { mygo.Clipboard.WriteText(code) })
			if cp.Hovered() {
				cp.Background(t.SurfaceHover)
			}
			cp.Children(func() { ui.Text(c, "Copy").FontSize(11).TextColor(t.TextMuted) })
		})
		ui.Box(c).Padding(10, 12).Children(func() {
			renderCodeLines(c, t, lang, code)
		})
	})
}

// renderCodeLines renders code as tight monospace lines with syntax
// highlighting.
func renderCodeLines(c *ui.Context, t *ui.Theme, lang, code string) {
	sc := syntaxColors(t.Dark)
	ui.Column(c).Gap(0).Children(func() {
		for _, line := range strings.Split(strings.TrimRight(code, "\n"), "\n") {
			spans := highlightLine(line, lang, sc)
			if len(spans) == 0 {
				spans = []ui.Span{{Text: "\u00a0"}}
			}
			ui.RichText(c, spans...).Font("monospace").FontSize(12.5).FixedLineHeight(18)
		}
	})
}

// inlineSpans parses inline Markdown into styled spans.
func inlineSpans(s string, t *ui.Theme) []ui.Span {
	var spans []ui.Span
	var b strings.Builder
	weight, italic, strike := 0, false, false
	flush := func() {
		if b.Len() == 0 {
			return
		}
		spans = append(spans, ui.Span{Text: b.String(), Weight: weight, Italic: italic, Strikethrough: strike})
		b.Reset()
	}
	i := 0
	for i < len(s) {
		switch {
		case strings.HasPrefix(s[i:], "**") || strings.HasPrefix(s[i:], "__"):
			flush()
			if weight == 700 {
				weight = 0
			} else {
				weight = 700
			}
			i += 2
		case strings.HasPrefix(s[i:], "~~"):
			flush()
			strike = !strike
			i += 2
		case s[i] == '`':
			end := strings.IndexByte(s[i+1:], '`')
			if end < 0 {
				b.WriteByte(s[i])
				i++
				continue
			}
			flush()
			spans = append(spans, ui.Span{Text: s[i+1 : i+1+end], Font: "monospace", Background: chipBg(t)})
			i += end + 2
		case s[i] == '*':
			flush()
			italic = !italic
			i++
		case s[i] == '!' && strings.HasPrefix(s[i:], "!["):
			close := strings.IndexByte(s[i+2:], ']')
			if close >= 0 {
				rest := s[i+2+close+1:]
				if strings.HasPrefix(rest, "(") {
					if end := strings.IndexByte(rest, ')'); end > 0 {
						alt := s[i+2 : i+2+close]
						if alt == "" {
							alt = "image"
						}
						flush()
						spans = append(spans, ui.Span{Text: "[image] " + alt, Color: t.TextMuted, Italic: true})
						i += 2 + close + 1 + end + 1
						continue
					}
				}
			}
			b.WriteByte(s[i])
			i++
		case s[i] == '[':
			close := strings.IndexByte(s[i:], ']')
			if close > 0 {
				rest := s[i+close+1:]
				if strings.HasPrefix(rest, "(") {
					if end := strings.IndexByte(rest, ')'); end > 0 {
						flush()
						spans = append(spans, ui.Span{Text: s[i+1 : i+close], Color: t.Accent, Underline: true})
						i += close + 1 + end + 1
						continue
					}
				}
			}
			b.WriteByte(s[i])
			i++
		case s[i] == '<':
			if end := strings.IndexByte(s[i:], '>'); end > 1 {
				url := s[i+1 : i+end]
				if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
					flush()
					spans = append(spans, ui.Span{Text: url, Color: t.Accent, Underline: true})
					i += end + 1
					continue
				}
			}
			b.WriteByte(s[i])
			i++
		default:
			if (strings.HasPrefix(s[i:], "http://") || strings.HasPrefix(s[i:], "https://")) &&
				(i == 0 || isURLBoundary(s[i-1])) {
				j := i
				for j < len(s) && !isURLEnd(s[j]) {
					j++
				}
				url := s[i:j]
				flush()
				spans = append(spans, ui.Span{Text: url, Color: t.Accent, Underline: true})
				i = j
				continue
			}
			b.WriteByte(s[i])
			i++
		}
	}
	flush()
	if len(spans) == 0 {
		return []ui.Span{{Text: s}}
	}
	return spans
}

func renderInline(c *ui.Context, t *ui.Theme, s string) ui.Element {
	return ui.RichText(c, inlineSpans(s, t)...)
}

func isURLBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '(', '[', '<', '{':
		return true
	}
	return false
}

func isURLEnd(b byte) bool {
	switch b {
	case ' ', '\t', '\n', ')', ']', '>', '"', '\'', '`':
		return true
	}
	return false
}

// --- syntax highlighting ---------------------------------------------------

type langRule struct {
	keywords     map[string]bool
	lineComments []string
	blockComment bool
	strings      bool
}

func highlightLine(line, lang string, sc syntaxSet) []ui.Span {
	rule, ok := langRules[normalizeLang(lang)]
	if !ok || line == "" {
		if line == "" {
			return nil
		}
		return []ui.Span{{Text: line}}
	}
	runes := []rune(line)
	var spans []ui.Span
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			spans = append(spans, ui.Span{Text: plain.String()})
			plain.Reset()
		}
	}
	add := func(s string, color ui.Color, weight int) {
		if s == "" {
			return
		}
		flush()
		spans = append(spans, ui.Span{Text: s, Color: color, Weight: weight})
	}
	i := 0
	for i < len(runes) {
		// Line comments.
		if s, ok := matchComment(runes, i, rule.lineComments); ok {
			add(string(runes[i:]), sc.comment, 0)
			i = len(runes)
			_ = s
			break
		}
		// Block comment start.
		if rule.blockComment && i+1 < len(runes) && runes[i] == '/' && runes[i+1] == '*' {
			j := i + 2
			for j+1 < len(runes) && !(runes[j] == '*' && runes[j+1] == '/') {
				j++
			}
			if j+1 < len(runes) {
				j += 2
			}
			add(string(runes[i:j]), sc.comment, 0)
			i = j
			continue
		}
		// Strings.
		if runes[i] == '"' || runes[i] == '\'' || runes[i] == '`' {
			quote := runes[i]
			j := i + 1
			for j < len(runes) {
				if runes[j] == '\\' && quote != '`' {
					j += 2
					continue
				}
				if runes[j] == quote {
					j++
					break
				}
				j++
			}
			add(string(runes[i:j]), sc.str, 0)
			i = j
			continue
		}
		// Numbers.
		if unicode.IsDigit(runes[i]) {
			j := i
			for j < len(runes) && (unicode.IsDigit(runes[j]) || unicode.IsLetter(runes[j]) || runes[j] == '.' || runes[j] == '_') {
				j++
			}
			add(string(runes[i:j]), sc.number, 0)
			i = j
			continue
		}
		// Identifiers and keywords.
		if isIdentStart(runes[i]) {
			j := i
			for j < len(runes) && isIdentPart(runes[j]) {
				j++
			}
			word := string(runes[i:j])
			if rule.keywords[word] {
				add(word, sc.keyword, 0)
			} else {
				plain.WriteString(word)
			}
			i = j
			continue
		}
		plain.WriteRune(runes[i])
		i++
	}
	flush()
	return spans
}

func matchComment(runes []rune, i int, markers []string) (string, bool) {
	for _, m := range markers {
		mr := []rune(m)
		if i+len(mr) <= len(runes) && string(runes[i:i+len(mr)]) == m {
			return m, true
		}
	}
	return "", false
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func normalizeLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(lang, " \t{"); i >= 0 {
		lang = lang[:i]
	}
	switch lang {
	case "golang":
		return "go"
	case "js", "jsx", "mjs", "cjs":
		return "javascript"
	case "ts", "tsx":
		return "typescript"
	case "py":
		return "python"
	case "rb":
		return "ruby"
	case "sh", "shell", "zsh":
		return "bash"
	case "rs":
		return "rust"
	case "yml":
		return "yaml"
	case "c++":
		return "cpp"
	case "cs":
		return "csharp"
	}
	return lang
}

func keywords(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

var langRules = map[string]langRule{
	"go": {
		keywords:     keywords("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false make new len cap append copy delete panic recover string int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 uintptr byte rune float32 float64 complex64 complex128 bool error any"),
		lineComments: []string{"//"}, blockComment: true, strings: true,
	},
	"javascript": {
		keywords:     keywords("break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new return super switch this throw try typeof var void while with yield async await of static get set true false null undefined"),
		lineComments: []string{"//"}, blockComment: true, strings: true,
	},
	"typescript": {
		keywords:     keywords("break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new return super switch this throw try typeof var void while with yield async await of static get set true false null undefined interface type enum implements namespace declare readonly public private protected abstract as any string number boolean void never unknown object symbol bigint"),
		lineComments: []string{"//"}, blockComment: true, strings: true,
	},
	"python": {
		keywords:     keywords("and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield True False None self match case"),
		lineComments: []string{"#"}, strings: true,
	},
	"ruby": {
		keywords:     keywords("def end if elsif else unless while until for do return class module begin rescue ensure yield nil true false self then case when and or not require"),
		lineComments: []string{"#"}, strings: true,
	},
	"bash": {
		keywords:     keywords("if then else elif fi for while until do done case esac function return local export readonly in echo cd exit source set unset shift trap true false"),
		lineComments: []string{"#"}, strings: true,
	},
	"rust": {
		keywords:     keywords("as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait type unsafe use where while true false"),
		lineComments: []string{"//"}, blockComment: true, strings: true,
	},
	"json": {
		keywords: keywords("true false null"),
		strings:  true,
	},
	"yaml": {
		keywords:     keywords("true false null yes no"),
		lineComments: []string{"#"}, strings: true,
	},
	"toml": {
		keywords:     keywords("true false"),
		lineComments: []string{"#"}, strings: true,
	},
	"java": {
		keywords:     keywords("abstract assert boolean break byte case catch char class const continue default do double else enum extends final finally float for if implements import instanceof int interface long native new package private protected public return short static strictfp super switch synchronized this throw throws transient try void volatile while true false null"),
		lineComments: []string{"//"}, blockComment: true, strings: true,
	},
	"c": {
		keywords:     keywords("auto break case char const continue default do double else enum extern float for goto if inline int long register restrict return short signed sizeof static struct switch typedef union unsigned void volatile while true false NULL"),
		lineComments: []string{"//"}, blockComment: true, strings: true,
	},
	"cpp": {
		keywords:     keywords("auto bool break case catch char class const constexpr continue default delete do double else enum explicit extern false float for friend if inline int long namespace new noexcept nullptr operator private protected public return short signed sizeof static struct switch template this throw true try typedef typename union unsigned using virtual void volatile while"),
		lineComments: []string{"//"}, blockComment: true, strings: true,
	},
	"csharp": {
		keywords:     keywords("abstract as base bool break byte case catch char class const continue decimal default delegate do double else enum event explicit extern false finally fixed float for foreach goto if implicit in int interface internal is lock long namespace new null object operator out override params private protected public readonly ref return sbyte sealed short sizeof stackalloc static string struct switch this throw true try typeof uint ulong unchecked unsafe ushort using virtual void volatile while var async await"),
		lineComments: []string{"//"}, blockComment: true, strings: true,
	},
	"sql": {
		keywords:     keywords("select from where insert update delete into values set join left right inner outer on group by order having limit offset as and or not null create table primary key foreign references index drop alter add distinct count sum avg min max union all case when then else end"),
		lineComments: []string{"--"}, strings: true,
	},
}
