package logx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

const (
	reset    = "\033[0m"
	bold     = "\033[1m"
	dim      = "\033[2m"
	red      = "\033[31m"
	green    = "\033[32m"
	yellow   = "\033[33m"
	blue     = "\033[34m"
	magenta  = "\033[35m"
	cyan     = "\033[36m"
	white    = "\033[37m"
	hiBlue   = "\033[94m"
	hiCyan   = "\033[96m"
	hiGreen  = "\033[92m"
	hiYellow = "\033[93m"
	hiRed    = "\033[91m"
)

func init() {
	enableWindowsANSI()
}

// Enabled reports whether ANSI colors should be used on w.
func Enabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("OROLLAMA_FORCE_COLOR") == "1" {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// NewLogger returns a pretty colored slog logger (falls back to plain text if not a TTY).
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	h := &Handler{
		w:     w,
		color: Enabled(w),
		level: level,
		mu:    &sync.Mutex{},
	}
	return slog.New(h)
}

// Handler is a compact colorful slog handler for local terminals.
type Handler struct {
	w     io.Writer
	color bool
	level slog.Level
	mu    *sync.Mutex
	attrs []slog.Attr
	group string
}

func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cp := *h
	cp.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &cp
}

func (h *Handler) WithGroup(name string) slog.Handler {
	cp := *h
	if h.group != "" {
		cp.group = h.group + "." + name
	} else {
		cp.group = name
	}
	return &cp
}

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	ts := r.Time.Local().Format("15:04:05")
	level := strings.ToUpper(r.Level.String())

	attrs := make([]slog.Attr, 0, len(h.attrs)+r.NumAttrs())
	attrs = append(attrs, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	var b strings.Builder
	if r.Message == "request" {
		h.writeRequest(&b, ts, attrs)
	} else {
		h.writeLine(&b, ts, level, r.Level, r.Message, attrs)
	}

	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *Handler) paint(code, s string) string {
	if !h.color {
		return s
	}
	return code + s + reset
}

func (h *Handler) levelColor(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return hiRed
	case level >= slog.LevelWarn:
		return hiYellow
	case level >= slog.LevelInfo:
		return hiGreen
	default:
		return dim
	}
}

func (h *Handler) writeLine(b *strings.Builder, ts, levelLabel string, level slog.Level, msg string, attrs []slog.Attr) {
	fmt.Fprintf(b, "%s %s %s",
		h.paint(dim, ts),
		h.paint(bold+h.levelColor(level), fmt.Sprintf("%-5s", levelLabel)),
		h.paint(bold+white, msg),
	)
	for _, a := range attrs {
		key := a.Key
		if h.group != "" {
			key = h.group + "." + key
		}
		fmt.Fprintf(b, " %s%s%s",
			h.paint(cyan, key),
			h.paint(dim, "="),
			h.paint(hiCyan, fmt.Sprint(a.Value.Any())),
		)
	}
	b.WriteByte('\n')
}

func attrInt(a slog.Attr) int {
	switch a.Value.Kind() {
	case slog.KindInt64:
		return int(a.Value.Int64())
	case slog.KindUint64:
		return int(a.Value.Uint64())
	default:
		if v, ok := a.Value.Any().(int); ok {
			return v
		}
	}
	return 0
}

func (h *Handler) writeRequest(b *strings.Builder, ts string, attrs []slog.Attr) {
	method, path, backend := "", "", ""
	status, dur := 0, int64(0)
	for _, a := range attrs {
		switch a.Key {
		case "method":
			method = fmt.Sprint(a.Value.Any())
		case "path":
			path = fmt.Sprint(a.Value.Any())
		case "status":
			status = attrInt(a)
		case "duration_ms":
			dur = int64(attrInt(a))
		case "backend":
			backend = fmt.Sprint(a.Value.Any())
		}
	}

	statusColor := hiGreen
	switch {
	case status >= 500:
		statusColor = hiRed
	case status >= 400:
		statusColor = hiYellow
	case status >= 300:
		statusColor = hiBlue
	}

	methodColor := magenta
	switch method {
	case "GET":
		methodColor = blue
	case "POST":
		methodColor = green
	case "DELETE":
		methodColor = red
	case "PUT", "PATCH":
		methodColor = yellow
	}

	fmt.Fprintf(b, "%s %s %s %s %s",
		h.paint(dim, ts),
		h.paint(bold+methodColor, fmt.Sprintf("%-6s", method)),
		h.paint(white, path),
		h.paint(bold+statusColor, fmt.Sprintf("%d", status)),
		h.paint(dim, fmt.Sprintf("%dms", dur)),
	)
	if backend != "" {
		fmt.Fprintf(b, " %s%s",
			h.paint(dim, "→ "),
			h.paint(hiCyan, backend),
		)
	}
	b.WriteByte('\n')
}

// RoleLine is a banner model row.
type RoleLine struct {
	ID   string
	Auto bool
}

func formatRole(id string, auto bool) string {
	if id == "" {
		return "(unset)"
	}
	if auto {
		return id + " (auto)"
	}
	return id
}

// Banner prints a colorful startup banner.
func Banner(w io.Writer, addr, ollamaURL, openRouterURL string, auth bool, opus, sonnet, haiku RoleLine) {
	color := Enabled(w)
	paint := func(code, s string) string {
		if !color {
			return s
		}
		return code + s + reset
	}

	line := strings.Repeat("─", 56)
	fmt.Fprintln(w, paint(hiCyan+bold, "╭"+line+"╮"))
	fmt.Fprintf(w, "%s %s  %s\n",
		paint(hiCyan+bold, "│"),
		paint(bold+hiGreen, "orollama"),
		paint(dim, "OpenRouter + Ollama proxy"),
	)
	fmt.Fprintln(w, paint(hiCyan+bold, "├"+line+"┤"))
	fmt.Fprintf(w, "%s %s   %s\n", paint(hiCyan+bold, "│"), paint(cyan, "listen"), paint(hiGreen, addr))
	fmt.Fprintf(w, "%s %s   %s\n", paint(hiCyan+bold, "│"), paint(cyan, "ollama"), paint(white, ollamaURL))
	fmt.Fprintf(w, "%s %s  %s\n", paint(hiCyan+bold, "│"), paint(cyan, "openrouter"), paint(white, openRouterURL))
	authLabel := paint(hiYellow, "off")
	if auth {
		authLabel = paint(hiGreen, "on")
	}
	fmt.Fprintf(w, "%s %s     %s\n", paint(hiCyan+bold, "│"), paint(cyan, "auth"), authLabel)
	fmt.Fprintln(w, paint(hiCyan+bold, "├"+line+"┤"))
	fmt.Fprintf(w, "%s %s     %s\n", paint(hiCyan+bold, "│"), paint(magenta, "opus"), paint(white, formatRole(opus.ID, opus.Auto)))
	fmt.Fprintf(w, "%s %s   %s\n", paint(hiCyan+bold, "│"), paint(magenta, "sonnet"), paint(white, formatRole(sonnet.ID, sonnet.Auto)))
	fmt.Fprintf(w, "%s %s    %s\n", paint(hiCyan+bold, "│"), paint(magenta, "haiku"), paint(white, formatRole(haiku.ID, haiku.Auto)))
	fmt.Fprintln(w, paint(hiCyan+bold, "╰"+line+"╯"))
}
