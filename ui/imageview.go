package ui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"editor/config"

	"github.com/gdamore/tcell/v2"
	"github.com/soniakeys/quant/median"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// ImageProtocol represents the terminal image rendering protocol to use.
type ImageProtocol int

const (
	ProtoHalfBlock ImageProtocol = iota // Unicode half-block fallback (works everywhere)
	ProtoSixel                          // Sixel graphics (foot, Konsole, Windows Terminal, explicit opt-in)
	ProtoKitty                          // Kitty graphics protocol (Kitty, WezTerm, Ghostty)
	ProtoITerm2                         // iTerm2 inline images (iTerm2, WezTerm, mintty)
	ProtoBraille                        // Braille dot patterns (2×4 per cell, more detail than half-block)
)

// DetectProtocol checks environment variables and the user's configured
// protocol preference to determine the image rendering protocol.
func DetectProtocol(configProtocol string) ImageProtocol {
	term := os.Getenv("TERM")
	termProgram := os.Getenv("TERM_PROGRAM")

	// User config setting takes priority (unless "auto")
	override := strings.ToLower(strings.TrimSpace(configProtocol))
	if override == "" || override == "auto" {
		// Fall back to env var
		override = strings.ToLower(strings.TrimSpace(os.Getenv("ALN_IMAGE_PROTOCOL")))
	}

	switch override {
	case "halfblock", "half-block", "unicode":
		return ProtoHalfBlock
	case "sixel":
		return ProtoSixel
	case "kitty":
		return ProtoKitty
	case "iterm2", "iterm":
		return ProtoITerm2
	case "braille":
		return ProtoBraille
	}

	// Kitty
	if term == "xterm-kitty" || os.Getenv("KITTY_INSTALLATION_DIR") != "" || os.Getenv("KITTY_PID") != "" || os.Getenv("KITTY_WINDOW_ID") != "" {
		return ProtoKitty
	}

	// Ghostty (supports Kitty protocol)
	if termProgram == "ghostty" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return ProtoKitty
	}

	// iTerm2
	if termProgram == "iTerm.app" {
		return ProtoITerm2
	}

	// WezTerm supports all three; prefer Kitty for best quality
	if termProgram == "WezTerm" || os.Getenv("WEZTERM_EXECUTABLE") != "" || os.Getenv("WEZTERM_PANE") != "" {
		return ProtoKitty
	}

	// Mintty (iTerm2 protocol)
	if termProgram == "mintty" || os.Getenv("MINTTY_SHORTCUT") != "" {
		return ProtoITerm2
	}

	// Foot terminal (Sixel)
	if termProgram == "foot" || strings.HasPrefix(term, "foot") {
		return ProtoSixel
	}

	// Konsole (recent versions support Sixel)
	if termProgram == "konsole" {
		return ProtoSixel
	}

	// Windows Terminal (Sixel since v1.22)
	if os.Getenv("WT_SESSION") != "" {
		return ProtoSixel
	}

	// Explicit opt-in for terminals that expose SIXEL support in TERM.
	if strings.Contains(strings.ToLower(term), "sixel") || os.Getenv("ALN_ENABLE_SIXEL") == "1" {
		return ProtoSixel
	}

	// xterm-like terminals frequently support SIXEL even when TERM is generic.
	// Users can force a different backend via ALN_IMAGE_PROTOCOL.
	if strings.HasPrefix(term, "xterm") {
		return ProtoSixel
	}

	return ProtoHalfBlock
}

// ImageView displays an image inside the editor area.
type ImageView struct {
	Path     string
	img      image.Image
	scaled   image.Image // cached scaled image
	encoded  []byte      // cached encoded protocol output (Sixel/Kitty/iTerm2)
	rendered bool        // true when encoded data has been written to TTY
	err      error
	focused  bool
	Theme    *config.ColorScheme
	protocol ImageProtocol
	active   ImageProtocol
	// last render dimensions for protocol rendering
	lastX, lastY, lastW, lastH int
	// computed cell placement for the image within the area
	imgCols, imgRows       int
	imgOffsetX, imgOffsetY int
	imgPixW, imgPixH       int
	tty                    *os.File
	bgColor                color.RGBA
	lastBGColor            color.RGBA
	lastPath               string
	// cached terminal pixel dimensions from TIOCGWINSZ
	winPixW, winPixH int
	winCols, winRows int
}

// NewImageView loads an image from the given path.
func NewImageView(path string, configProtocol string) *ImageView {
	iv := &ImageView{
		Path:     path,
		protocol: DetectProtocol(configProtocol),
		bgColor:  color.RGBA{0, 0, 0, 255}, // safe default until theme is set
	}

	f, err := os.Open(path)
	if err != nil {
		iv.err = err
		return iv
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		iv.err = fmt.Errorf("unsupported image format: %v", err)
		return iv
	}
	iv.img = img
	return iv
}

func (iv *ImageView) SetProtocol(configProtocol string) {
	proto := DetectProtocol(configProtocol)
	if iv.protocol != proto {
		iv.protocol = proto
		iv.InvalidateRender()
	}
}

// IsImageFile returns true if the file extension indicates a common image format.
func IsImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".tif", ".webp", ".ico":
		return true
	}
	return false
}

type imagePlacement struct {
	cols, rows       int
	offsetX, offsetY int
	pixelW, pixelH   int
}

func (iv *ImageView) effectiveProtocol() ImageProtocol {
	return iv.protocol
}

func (iv *ImageView) SetTheme(theme *config.ColorScheme) {
	iv.Theme = theme
	if theme != nil {
		iv.bgColor = colorToRGBA(theme.Background)
	}
}

func (iv *ImageView) layoutForArea(cellW, cellH int, proto ImageProtocol) imagePlacement {
	var p imagePlacement
	if iv.img == nil || cellW < 1 || cellH < 1 {
		return p
	}

	bounds := iv.img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW < 1 || srcH < 1 {
		return p
	}

	cellPixW, cellPixH := iv.layoutCellPixelSize(proto)
	cellAspect := float64(cellPixW) / float64(cellPixH)
	if cellAspect <= 0 {
		cellAspect = 0.5
	}

	imgAspect := float64(srcW) / float64(srcH)
	if imgAspect <= 0 {
		imgAspect = 1
	}

	// Orientation-first fit:
	// - landscape/square images try to fill editor width
	// - portrait images try to fill editor height
	// If the chosen axis would overflow, fall back to the other axis.
	landscape := srcW >= srcH
	if landscape {
		p.cols = cellW
		p.rows = int(math.Round(float64(p.cols) * cellAspect / imgAspect))
		if p.rows > cellH {
			p.rows = cellH
			p.cols = int(math.Round(float64(p.rows) * imgAspect / cellAspect))
		}
	} else {
		p.rows = cellH
		p.cols = int(math.Round(float64(p.rows) * imgAspect / cellAspect))
		if p.cols > cellW {
			p.cols = cellW
			p.rows = int(math.Round(float64(p.cols) * cellAspect / imgAspect))
		}
	}

	if p.cols < 1 {
		p.cols = 1
	}
	if p.rows < 1 {
		p.rows = 1
	}
	if p.cols > cellW {
		p.cols = cellW
	}
	if p.rows > cellH {
		p.rows = cellH
	}

	if proto == ProtoHalfBlock {
		p.offsetX = centerOffset(cellW, p.cols)
		p.offsetY = centerOffset(cellH, p.rows)
		p.pixelW = p.cols
		p.pixelH = p.rows * 2
	} else if proto == ProtoBraille {
		p.offsetX = centerOffset(cellW, p.cols)
		p.offsetY = centerOffset(cellH, p.rows)
		p.pixelW = p.cols * 2 // 2 dots wide per cell
		p.pixelH = p.rows * 4 // 4 dots tall per cell
	} else {
		scaleX, scaleY := protocolScaleXY(proto)
		p.offsetX = centerOffset(cellW, p.cols)
		p.offsetY = centerOffset(cellH, p.rows)
		// Derive pixel dimensions from window pixel size when available.
		// This avoids rounding errors from guessing cell pixel size and
		// multiplying, which is unreliable over SSH/conpty.
		if iv.winPixW > 0 && iv.winPixH > 0 && iv.winCols > 0 && iv.winRows > 0 {
			p.pixelW = int(math.Round(float64(iv.winPixW) * float64(p.cols) / float64(iv.winCols) * scaleX))
			p.pixelH = int(math.Round(float64(iv.winPixH) * float64(p.rows) / float64(iv.winRows) * scaleY))
		} else {
			p.pixelW = int(math.Round(float64(p.cols*cellPixW) * scaleX))
			p.pixelH = int(math.Round(float64(p.rows*cellPixH) * scaleY))
		}
	}
	if p.pixelW < 1 {
		p.pixelW = 1
	}
	if p.pixelH < 1 {
		p.pixelH = 1
	}

	return p
}

func (iv *ImageView) layoutCellPixelSize(proto ImageProtocol) (int, int) {
	if proto == ProtoSixel {
		if w, h, ok := envCellPixelSize("ALN_SIXEL"); ok {
			return w, h
		}
	}
	if w, h, ok := envCellPixelSize("ALN_IMAGE"); ok {
		return w, h
	}

	pw, ph, ok := iv.getCellPixelSize()
	if ok && validCellRatio(pw, ph) {
		return pw, ph
	}

	// Conservative protocol-specific defaults when pixel introspection is unavailable.
	// Sixel often needs slightly larger pixels in SSH/conpty chains.
	if proto == ProtoSixel {
		return 10, 20
	}
	return 8, 16
}

func validCellRatio(w, h int) bool {
	if w < 1 || h < 1 {
		return false
	}
	ratio := float64(w) / float64(h)
	return ratio >= 0.30 && ratio <= 0.80
}

func envCellPixelSize(prefix string) (int, int, bool) {
	if raw := strings.TrimSpace(os.Getenv(prefix + "_CELL_PIXELS")); raw != "" {
		if w, h, ok := parseCellPixels(raw); ok {
			return w, h, true
		}
	}

	wRaw := strings.TrimSpace(os.Getenv(prefix + "_CELL_WIDTH"))
	hRaw := strings.TrimSpace(os.Getenv(prefix + "_CELL_HEIGHT"))
	if wRaw != "" && hRaw != "" {
		w, wErr := strconv.Atoi(wRaw)
		h, hErr := strconv.Atoi(hRaw)
		if wErr == nil && hErr == nil && w > 0 && h > 0 {
			return w, h, true
		}
	}
	return 0, 0, false
}

func parseCellPixels(raw string) (int, int, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, ",", "x")
	s = strings.ReplaceAll(s, " ", "")
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, wErr := strconv.Atoi(parts[0])
	h, hErr := strconv.Atoi(parts[1])
	if wErr != nil || hErr != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

func protocolScaleXY(proto ImageProtocol) (float64, float64) {
	sx, sy := 1.0, 1.0

	// Global overrides.
	sx, sy = scaleFromEnvPair("ALN_IMAGE", sx, sy, true)

	// Protocol-specific overrides.
	if proto == ProtoSixel {
		// Backward-compat note: ALN_SIXEL_SCALE intentionally calibrates X only.
		// Most WT+SSH mismatch is horizontal, and this avoids accidental vertical overflow.
		sx, sy = scaleFromEnvPair("ALN_SIXEL", sx, sy, false)
	}

	return sx, sy
}

func scaleFromEnvPair(prefix string, curX, curY float64, uniformScalar bool) (float64, float64) {
	xName := prefix + "_SCALE_X"
	yName := prefix + "_SCALE_Y"
	sName := prefix + "_SCALE"

	xVal, xSet := parseScaleEnv(xName)
	yVal, ySet := parseScaleEnv(yName)
	if xSet {
		curX = xVal
	}
	if ySet {
		curY = yVal
	}
	if sVal, sSet := parseScaleEnv(sName); sSet {
		if !xSet {
			curX = sVal
		}
		if !ySet {
			if uniformScalar {
				curY = sVal
			} else {
				curY = 1
			}
		}
	}
	return curX, curY
}

func parseScaleEnv(name string) (float64, bool) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	if v < 0.25 {
		v = 0.25
	}
	if v > 4 {
		v = 4
	}
	return v, true
}

func centerOffset(total, used int) int {
	if used >= total {
		return 0
	}
	return int(math.Round(float64(total-used) / 2))
}

func (iv *ImageView) Render(screen tcell.Screen, x, y, width, height int) {
	theme := iv.Theme
	bgStyle := tcell.StyleDefault
	if theme != nil {
		bgStyle = bgStyle.Background(theme.Background).Foreground(theme.Foreground)
	}

	// Always clear the full editor area first.
	for row := y; row < y+height; row++ {
		for col := x; col < x+width; col++ {
			screen.SetContent(col, row, ' ', nil, bgStyle)
		}
	}

	if iv.err != nil {
		msg := fmt.Sprintf("  ⚠ Cannot display image: %s", iv.err.Error())
		for i, ch := range msg {
			if i < width {
				screen.SetContent(x+i, y+height/2, ch, nil, bgStyle)
			}
		}
		return
	}

	if iv.img == nil {
		return
	}

	if width < 1 || height < 1 {
		return
	}

	proto := iv.effectiveProtocol()
	placement := iv.layoutForArea(width, height, proto)
	if placement.cols < 1 || placement.rows < 1 {
		return
	}

	changed := iv.Path != iv.lastPath ||
		x != iv.lastX || y != iv.lastY || width != iv.lastW || height != iv.lastH ||
		proto != iv.active ||
		iv.bgColor != iv.lastBGColor ||
		placement.cols != iv.imgCols || placement.rows != iv.imgRows ||
		placement.offsetX != iv.imgOffsetX || placement.offsetY != iv.imgOffsetY ||
		placement.pixelW != iv.imgPixW || placement.pixelH != iv.imgPixH

	iv.lastPath = iv.Path
	iv.lastBGColor = iv.bgColor
	iv.active = proto
	iv.lastX = x
	iv.lastY = y
	iv.lastW = width
	iv.lastH = height
	iv.imgCols = placement.cols
	iv.imgRows = placement.rows
	iv.imgOffsetX = placement.offsetX
	iv.imgOffsetY = placement.offsetY
	iv.imgPixW = placement.pixelW
	iv.imgPixH = placement.pixelH

	if changed {
		iv.scaled = nil
		iv.encoded = nil
		iv.rendered = false
	}

	if proto == ProtoHalfBlock {
		iv.renderHalfBlock(screen, x+placement.offsetX, y+placement.offsetY, placement.cols, placement.rows)
	} else if proto == ProtoBraille {
		iv.renderBraille(screen, x+placement.offsetX, y+placement.offsetY, placement.cols, placement.rows)
	}
}

// RenderProtocolImage writes the image using terminal graphics protocols.
// Must be called AFTER screen.Show() since it writes raw escape sequences.
// Only re-writes to TTY when the encoded data has changed or after a Sync().
func (iv *ImageView) RenderProtocolImage() {
	if iv.active == ProtoHalfBlock || iv.img == nil || iv.err != nil {
		return
	}

	tty := iv.getTTY()
	if tty == nil {
		return
	}

	if iv.encoded == nil {
		if iv.scaled == nil {
			iv.scaled = iv.fitImage(iv.imgPixW, iv.imgPixH)
		}
		if iv.scaled == nil {
			return
		}
		iv.encoded = iv.encodeProtocolImage(iv.scaled)
		iv.rendered = false
	}
	if len(iv.encoded) == 0 {
		return
	}

	// Skip re-writing if the exact same encoded data was already sent.
	if iv.rendered {
		return
	}

	// Save and restore cursor so protocol rendering doesn't disturb tcell cursor state.
	fmt.Fprint(tty, "\0337")
	fmt.Fprintf(tty, "\033[%d;%dH", iv.lastY+iv.imgOffsetY+1, iv.lastX+iv.imgOffsetX+1)
	tty.Write(iv.encoded)
	fmt.Fprint(tty, "\0338")
	iv.rendered = true
}

// NeedsProtocolRender returns true if this view uses a graphics protocol
// (not half-block/braille) and should call RenderProtocolImage() after Show().
func (iv *ImageView) NeedsProtocolRender() bool {
	return iv.active != ProtoHalfBlock && iv.active != ProtoBraille && iv.img != nil && iv.err == nil
}

// MarkDirty forces the next RenderProtocolImage call to re-write the encoded
// data to the TTY, without re-encoding the image. Use after screen.Sync()
// which overwrites the sixel graphics plane with text cells.
func (iv *ImageView) MarkDirty() {
	iv.rendered = false
}

// InvalidateRender clears cached image data, forcing re-encode on next render
// (e.g. after a screen resize). Also clears cached cell pixel dimensions so
// they are re-queried from the terminal.
func (iv *ImageView) InvalidateRender() {
	iv.scaled = nil
	iv.encoded = nil
	iv.rendered = false
	iv.imgCols = 0
	iv.imgRows = 0
	iv.imgOffsetX = 0
	iv.imgOffsetY = 0
	iv.imgPixW = 0
	iv.imgPixH = 0
	iv.winPixW = 0
	iv.winPixH = 0
	iv.winCols = 0
	iv.winRows = 0
}

// encodeProtocolImage encodes the scaled image into the appropriate protocol
// format and returns the raw bytes to write to the tty. This is expensive
// (especially Sixel) so the result is cached.
func (iv *ImageView) encodeProtocolImage(img image.Image) []byte {
	var buf bytes.Buffer
	switch iv.active {
	case ProtoKitty:
		iv.encodeKitty(&buf, img)
	case ProtoITerm2:
		iv.encodeITerm2(&buf, img)
	case ProtoSixel:
		iv.encodeSixel(&buf, img)
	}
	return buf.Bytes()
}

func (iv *ImageView) HandleKey(ev *tcell.EventKey) bool {
	return false // editor handles Esc to close
}

func (iv *ImageView) HandleMouse(ev *tcell.EventMouse) bool {
	return false
}

// ImageSize returns the original image dimensions, or (0,0) if not loaded.
func (iv *ImageView) ImageSize() (int, int) {
	if iv.img == nil {
		return 0, 0
	}
	b := iv.img.Bounds()
	return b.Dx(), b.Dy()
}

func (iv *ImageView) IsFocused() bool {
	return iv.focused
}

func (iv *ImageView) SetFocused(f bool) {
	iv.focused = f
}

// ClearProtocolImage sends cleanup escape sequences to remove any protocol-rendered
// image from the screen (e.g. when switching away from this tab).
func (iv *ImageView) ClearProtocolImage() {
	if iv.lastW <= 0 || iv.lastH <= 0 {
		return
	}
	tty := iv.getTTY()
	if tty == nil {
		return
	}
	if iv.active == ProtoKitty || iv.protocol == ProtoKitty {
		fmt.Fprintf(tty, "\033_Ga=d;\033\\")
	}
	if iv.active == ProtoSixel && iv.imgPixW > 0 && iv.imgPixH > 0 {
		// Overwrite the sixel graphics plane with a solid-color sixel.
		// Writing text/spaces does NOT clear the sixel layer in many terminals.
		bg := iv.bgColor
		r := uint(bg.R) * 100 / 255
		g := uint(bg.G) * 100 / 255
		b := uint(bg.B) * 100 / 255
		var buf bytes.Buffer
		fmt.Fprint(&buf, "\033[?80l") // ensure scrolling mode
		fmt.Fprintf(&buf, "\033P0;0;8q\"1;1;%d;%d#0;2;%d;%d;%d", iv.imgPixW, iv.imgPixH, r, g, b)
		// Fill every sixel row with the background color using RLE
		row := fmt.Sprintf("!%d~", iv.imgPixW) // '~' = all 6 bits set
		for z := 0; z < (iv.imgPixH+5)/6; z++ {
			if z > 0 {
				buf.WriteByte('-') // DECGNL: next sixel row
			}
			fmt.Fprintf(&buf, "#0%s", row)
		}
		buf.WriteString("\033\\") // ST: end sixel
		// Position cursor and write the clearing sixel
		fmt.Fprint(tty, "\0337") // save cursor
		fmt.Fprintf(tty, "\033[%d;%dH", iv.lastY+iv.imgOffsetY+1, iv.lastX+iv.imgOffsetX+1)
		tty.Write(buf.Bytes())
		fmt.Fprint(tty, "\0338") // restore cursor
	}
	iv.rendered = false
}

// Close cleans up resources.
func (iv *ImageView) Close() {
	// Clear Kitty images if we used that protocol
	if iv.active == ProtoKitty || iv.protocol == ProtoKitty {
		if tty := iv.getTTY(); tty != nil {
			fmt.Fprintf(tty, "\033_Ga=d;\033\\")
		}
	}
	if iv.tty != nil {
		iv.tty.Close()
		iv.tty = nil
	}
}

// getTTY opens /dev/tty for raw escape sequence output.
func (iv *ImageView) getTTY() *os.File {
	if iv.tty != nil {
		return iv.tty
	}
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return nil
	}
	iv.tty = f
	return f
}

// getCellPixelSize attempts to determine the pixel dimensions of a terminal cell.
// Returns (cellW, cellH, ok) where ok reports whether the value came from
// terminal-reported pixel dimensions. Caches window-level pixel sizes for
// use in protocol pixel dimension calculations.
func (iv *ImageView) getCellPixelSize() (int, int, bool) {
	// Return cached result if available (refreshed on InvalidateRender)
	if iv.winPixW > 0 && iv.winPixH > 0 && iv.winCols > 0 && iv.winRows > 0 {
		cellW := iv.winPixW / iv.winCols
		cellH := iv.winPixH / iv.winRows
		if cellW > 0 && cellH > 0 {
			return cellW, cellH, true
		}
	}

	tty := iv.getTTY()
	if tty == nil {
		return 8, 16, false
	}
	// Try ioctl TIOCGWINSZ which returns rows, cols, xpixel, ypixel
	var ws [8]byte // struct winsize: 4 x uint16
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), 0x5413, uintptr(unsafe.Pointer(&ws[0])))
	if errno == 0 {
		rows := int(uint16(ws[0]) | uint16(ws[1])<<8)
		cols := int(uint16(ws[2]) | uint16(ws[3])<<8)
		xpix := int(uint16(ws[4]) | uint16(ws[5])<<8)
		ypix := int(uint16(ws[6]) | uint16(ws[7])<<8)
		if cols > 0 && rows > 0 {
			iv.winCols = cols
			iv.winRows = rows
		}
		if xpix > 0 && ypix > 0 {
			iv.winPixW = xpix
			iv.winPixH = ypix
		}
		if cols > 0 && rows > 0 && xpix > 0 && ypix > 0 {
			cellW := xpix / cols
			cellH := ypix / rows
			if cellW > 0 && cellH > 0 {
				return cellW, cellH, true
			}
		}
	}
	return 8, 16, false
}

// fitImage scales the source image to the exact destination pixel size.
func (iv *ImageView) fitImage(dstW, dstH int) image.Image {
	if iv.img == nil {
		return nil
	}
	if dstW < 1 || dstH < 1 {
		return nil
	}

	bounds := iv.img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return nil
	}
	if dstW == srcW && dstH == srcH {
		return iv.img
	}
	return resizeImage(iv.img, dstW, dstH)
}

// resizeImage performs nearest-neighbor scaling (fast, no dependencies).
// Alpha is preserved so protocol encoders can decide how to handle transparency.
func resizeImage(src image.Image, dstW, dstH int) image.Image {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		srcY := bounds.Min.Y + y*srcH/dstH
		for x := 0; x < dstW; x++ {
			srcX := bounds.Min.X + x*srcW/dstW
			r, g, b, a := src.At(srcX, srcY).RGBA()
			dst.SetRGBA(x, y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)})
		}
	}
	return dst
}

// renderHalfBlock renders the image using Unicode half-block characters (▀ U+2580).
// Each terminal cell represents 2 vertical pixels: top pixel as foreground,
// bottom pixel as background. This works on ALL terminals with true color.
func (iv *ImageView) renderHalfBlock(screen tcell.Screen, x, y, w, h int) {
	if iv.img == nil || w < 1 || h < 1 {
		return
	}

	bounds := iv.img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW < 1 || srcH < 1 {
		return
	}

	pseudoH := h * 2 // each cell row represents two vertical subpixels
	for cy := 0; cy < h; cy++ {
		topSrcY := bounds.Min.Y + (cy*2)*srcH/pseudoH
		botSrcY := bounds.Min.Y + (cy*2+1)*srcH/pseudoH
		if topSrcY >= bounds.Max.Y {
			topSrcY = bounds.Max.Y - 1
		}
		if botSrcY >= bounds.Max.Y {
			botSrcY = bounds.Max.Y - 1
		}
		for cx := 0; cx < w; cx++ {
			srcX := bounds.Min.X + cx*srcW/w
			if srcX >= bounds.Max.X {
				srcX = bounds.Max.X - 1
			}

			top := blendOverBackground(iv.img.At(srcX, topSrcY), iv.bgColor)
			bot := blendOverBackground(iv.img.At(srcX, botSrcY), iv.bgColor)

			fg := tcell.NewRGBColor(int32(top.R), int32(top.G), int32(top.B))
			bg := tcell.NewRGBColor(int32(bot.R), int32(bot.G), int32(bot.B))
			style := tcell.StyleDefault.Foreground(fg).Background(bg)
			screen.SetContent(x+cx, y+cy, '▀', nil, style)
		}
	}
}

// renderBraille renders the image using Unicode braille characters (U+2800-U+28FF).
// Each terminal cell represents a 2×4 dot grid, providing higher detail than half-block.
// Dots are set where the pixel differs significantly from the background; the
// foreground color is the average of the set-dot pixels.
func (iv *ImageView) renderBraille(screen tcell.Screen, x, y, w, h int) {
	if iv.img == nil || w < 1 || h < 1 {
		return
	}

	bounds := iv.img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW < 1 || srcH < 1 {
		return
	}

	bg := iv.bgColor
	bgLum := luminance(bg.R, bg.G, bg.B)

	// Braille dot positions within a cell (col, row) → bit
	// Col 0: rows 0-3 → bits 0,1,2,6
	// Col 1: rows 0-3 → bits 3,4,5,7
	dotBit := [4][2]rune{
		{0x01, 0x08},
		{0x02, 0x10},
		{0x04, 0x20},
		{0x40, 0x80},
	}

	subW := w * 2 // 2 dots per cell horizontally
	subH := h * 4 // 4 dots per cell vertically

	for cy := 0; cy < h; cy++ {
		for cx := 0; cx < w; cx++ {
			var pattern rune
			var rSum, gSum, bSum uint32
			var nSet uint32

			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					subX := cx*2 + dx
					subY := cy*4 + dy
					srcX := bounds.Min.X + subX*srcW/subW
					srcY := bounds.Min.Y + subY*srcH/subH
					if srcX >= bounds.Max.X {
						srcX = bounds.Max.X - 1
					}
					if srcY >= bounds.Max.Y {
						srcY = bounds.Max.Y - 1
					}

					c := blendOverBackground(iv.img.At(srcX, srcY), bg)
					lum := luminance(c.R, c.G, c.B)

					// Set dot if pixel differs enough from background
					diff := int(lum) - int(bgLum)
					if diff < 0 {
						diff = -diff
					}
					if diff > 30 {
						pattern |= dotBit[dy][dx]
						rSum += uint32(c.R)
						gSum += uint32(c.G)
						bSum += uint32(c.B)
						nSet++
					}
				}
			}

			ch := '\u2800' + pattern
			style := tcell.StyleDefault.Background(tcell.NewRGBColor(int32(bg.R), int32(bg.G), int32(bg.B)))
			if nSet > 0 {
				avgR := int32(rSum / nSet)
				avgG := int32(gSum / nSet)
				avgB := int32(bSum / nSet)
				style = style.Foreground(tcell.NewRGBColor(avgR, avgG, avgB))
			}
			screen.SetContent(x+cx, y+cy, ch, nil, style)
		}
	}
}

func luminance(r, g, b uint8) uint8 {
	return uint8((uint32(r)*299 + uint32(g)*587 + uint32(b)*114) / 1000)
}

func blendOverBackground(src color.Color, bg color.RGBA) color.RGBA {
	c := color.NRGBAModel.Convert(src).(color.NRGBA)
	if c.A == 255 {
		return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255}
	}
	if c.A == 0 {
		return bg
	}
	a := uint32(c.A)
	inv := uint32(255 - c.A)
	return color.RGBA{
		R: uint8((uint32(c.R)*a + uint32(bg.R)*inv + 127) / 255),
		G: uint8((uint32(c.G)*a + uint32(bg.G)*inv + 127) / 255),
		B: uint8((uint32(c.B)*a + uint32(bg.B)*inv + 127) / 255),
		A: 255,
	}
}

func colorToRGBA(c tcell.Color) color.RGBA {
	if c == tcell.ColorDefault {
		return color.RGBA{0, 0, 0, 255}
	}
	r, g, b := c.RGB()
	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}

// encodeKitty encodes an image using the Kitty graphics protocol.
func (iv *ImageView) encodeKitty(w *bytes.Buffer, img image.Image) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	rgba := iv.imageToRGBA(img)
	raw := rgba.Pix

	b64 := base64.StdEncoding.EncodeToString(raw)

	const chunkSize = 4096
	for i := 0; i < len(b64); i += chunkSize {
		end := i + chunkSize
		if end > len(b64) {
			end = len(b64)
		}
		chunk := b64[i:end]
		more := 1
		if end >= len(b64) {
			more = 0
		}

		if i == 0 {
			fmt.Fprintf(w, "\033_Ga=T,f=32,s=%d,v=%d,c=%d,r=%d,m=%d;%s\033\\",
				width, height, iv.imgCols, iv.imgRows, more, chunk)
		} else {
			fmt.Fprintf(w, "\033_Gm=%d;%s\033\\", more, chunk)
		}
	}
}

// encodeITerm2 encodes an image using the iTerm2 inline image protocol.
func (iv *ImageView) encodeITerm2(w *bytes.Buffer, img image.Image) {
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, iv.imageToRGBA(img)); err != nil {
		return
	}
	b64 := base64.StdEncoding.EncodeToString(pngBuf.Bytes())
	fmt.Fprintf(w, "\033]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=0:%s\a",
		iv.imgCols, iv.imgRows, b64)
}

// encodeSixel encodes an image using the Sixel protocol with P2=1 (transparent
// background). Transparent/semi-transparent pixels are left unset so the
// terminal's own background shows through, avoiding color mismatch from the
// lossy sixel percentage palette. Semi-transparent pixels are composited
// against the theme background to produce correct blending.
func (iv *ImageView) encodeSixel(w *bytes.Buffer, img image.Image) {
	rgba := iv.imageToRGBAPreserveAlpha(img)
	if rgba == nil {
		return
	}
	width := rgba.Bounds().Dx()
	height := rgba.Bounds().Dy()
	if width == 0 || height == 0 {
		return
	}

	// Ensure scrolling mode so the image is anchored at the current cursor position.
	fmt.Fprint(w, "\033[?80l")

	// Quantize to 254 colors (palette entries 1..254; entry 0 is unused)
	const nc = 255
	q := median.Quantizer(nc - 1)
	paletted := q.Paletted(rgba)
	draw.Draw(paletted, rgba.Bounds(), rgba, image.Point{}, draw.Over)

	// DCS with P2=1: transparent background — unset pixels show terminal bg
	fmt.Fprintf(w, "\033P0;1;8q\"1;1;%d;%d", width, height)

	// Register palette using rounded percentage conversion for accuracy
	for n, v := range paletted.Palette {
		r, g, b, _ := v.RGBA()
		rp := (r*100 + 0x7FFF) / 0xFFFF
		gp := (g*100 + 0x7FFF) / 0xFFFF
		bp := (b*100 + 0x7FFF) / 0xFFFF
		fmt.Fprintf(w, "#%d;2;%d;%d;%d", n+1, rp, gp, bp)
	}

	// Encode sixel bands (6 pixel rows each)
	buf := make([]byte, width*nc)
	cset := make([]bool, nc)
	first := true
	for z := 0; z < (height+5)/6; z++ {
		if !first {
			w.WriteByte('-') // DECGNL: next line
		}
		first = false

		// Build bit patterns for each color in this band
		for p := 0; p < 6; p++ {
			y := z*6 + p
			if y >= height {
				break
			}
			for x := 0; x < width; x++ {
				c := rgba.RGBAAt(x, y)
				if c.A < 128 {
					continue // transparent — leave unset for terminal bg
				}
				idx := int(paletted.ColorIndexAt(x, y)) + 1
				if idx >= nc {
					continue
				}
				cset[idx] = false
				buf[width*idx+x] |= 1 << uint(p)
			}
		}

		// Emit sixel data for each used color with RLE
		firstColor := true
		for n := 1; n < nc; n++ {
			if cset[n] {
				continue
			}
			cset[n] = true

			if !firstColor {
				w.WriteByte('$') // DECGCR: carriage return
			}
			firstColor = false

			// Select color
			fmt.Fprintf(w, "#%d", n)

			// RLE encode the sixel row
			cnt := 0
			var prev byte = 0xFF // impossible sixel value
			for x := 0; x < width; x++ {
				ch := buf[width*n+x]
				buf[width*n+x] = 0
				if ch == prev {
					cnt++
				} else {
					if cnt > 0 {
						writeSixelRun(w, prev, cnt)
					}
					prev = ch
					cnt = 1
				}
			}
			if cnt > 0 {
				writeSixelRun(w, prev, cnt)
			}
		}
	}

	w.Write([]byte{0x1b, 0x5c}) // ST: string terminator
}

// writeSixelRun writes a run of identical sixel characters with RLE.
func writeSixelRun(w *bytes.Buffer, ch byte, count int) {
	s := byte(63 + ch)
	if count == 1 {
		w.WriteByte(s)
	} else if count == 2 {
		w.WriteByte(s)
		w.WriteByte(s)
	} else if count == 3 {
		w.WriteByte(s)
		w.WriteByte(s)
		w.WriteByte(s)
	} else {
		fmt.Fprintf(w, "!%d%c", count, s)
	}
}

// imageToRGBAPreserveAlpha converts an image to RGBA, compositing semi-transparent
// pixels against the theme bg but preserving fully transparent pixels (alpha=0)
// for the sixel P2=1 transparency path.
func (iv *ImageView) imageToRGBAPreserveAlpha(img image.Image) *image.RGBA {
	if img == nil {
		return nil
	}
	bg := iv.bgColor
	if bg.A == 0 {
		bg = color.RGBA{40, 40, 40, 255}
	}
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A == 0 {
				// Fully transparent — leave as zero (transparent for P2=1)
				continue
			}
			if c.A == 255 {
				rgba.SetRGBA(x, y, color.RGBA{c.R, c.G, c.B, 255})
			} else {
				// Semi-transparent — blend against theme bg
				rgba.SetRGBA(x, y, blendOverBackground(img.At(x, y), bg))
			}
		}
	}
	return rgba
}

func (iv *ImageView) imageToRGBA(img image.Image) *image.RGBA {
	if img == nil {
		return nil
	}
	bg := iv.bgColor
	if bg.A == 0 {
		bg = color.RGBA{40, 40, 40, 255}
	}
	if rgba, ok := img.(*image.RGBA); ok {
		return compositeBackground(rgba, bg)
	}
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	return compositeBackground(rgba, bg)
}

func compositeBackground(img *image.RGBA, bg color.RGBA) *image.RGBA {
	bounds := img.Bounds()
	result := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			result.SetRGBA(x, y, blendOverBackground(img.RGBAAt(x, y), bg))
		}
	}
	return result
}
