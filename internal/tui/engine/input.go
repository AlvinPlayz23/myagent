package engine

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Key is a decoded key press. Code carries a canonical name; Text carries the
// produced text for editable input (runes, or "" for control keys).
type Key struct {
	Code string // "enter", "esc", "up", "a", "ctrl+c", "shift+enter", …
	Text string // printable text this key inserts, may be ""
	Mods Mods
}

// Mods is the modifier bitmask for a key event.
type Mods uint8

// Modifier bits.
const (
	ModShift Mods = 1 << iota
	ModAlt
	ModCtrl
)

// MouseAction discriminates mouse events.
type MouseAction uint8

// Mouse actions.
const (
	MousePress MouseAction = iota
	MouseDrag
	MouseRelease
	MouseWheelUp
	MouseWheelDown
)

// Mouse is a decoded mouse event in screen coordinates.
type Mouse struct {
	X, Y   int
	Button int // 0 left, 1 middle, 2 right
	Action MouseAction
}

// Paste carries bracketed-paste text.
type Paste struct {
	Text string
}

// Focus is a terminal focus change.
type Focus struct{ Gained bool }

// Event is any decoded input event.
type Event struct {
	Key   *Key
	Mouse *Mouse
	Paste *Paste
	Focus *Focus
}

// Decoder reads the tty byte stream and emits Events. Lone-Esc detection
// waits for a following sequence before reporting a bare Esc.
type Decoder struct {
	out      chan Event
	buf      []byte
	escDelay time.Duration

	mu     sync.Mutex
	closed bool
}

// NewDecoder starts a goroutine decoding f into the returned channel.
func NewDecoder(f *os.File) *Decoder {
	d := &Decoder{out: make(chan Event, 256), escDelay: 40 * time.Millisecond}
	go d.readLoop(f)
	return d
}

// Events exposes the decoded event stream.
func (d *Decoder) Events() <-chan Event { return d.out }

func (d *Decoder) readLoop(f *os.File) {
	chunk := make([]byte, 4096)
	for {
		n, err := f.Read(chunk)
		if n > 0 {
			d.mu.Lock()
			d.buf = append(d.buf, chunk[:n]...)
			d.mu.Unlock()
			d.drain()
		}
		if err != nil {
			d.mu.Lock()
			d.closed = true
			d.mu.Unlock()
			close(d.out)
			return
		}
	}
}

func (d *Decoder) drain() {
	for {
		d.mu.Lock()
		ev, used := d.decodeOne()
		if used == 0 {
			d.mu.Unlock()
			return
		}
		d.buf = d.buf[used:]
		closed := d.closed
		d.mu.Unlock()
		if ev != nil && !closed {
			d.out <- *ev
		}
	}
}

// decodeOne decodes the first event in the buffer, returning bytes consumed
// (0 = need more input).
func (d *Decoder) decodeOne() (*Event, int) {
	b := d.buf
	if len(b) == 0 {
		return nil, 0
	}
	// Incomplete CSI/SS3/OSC prefix: wait for the terminator.
	if b[0] == 0x1b && len(b) < 2 {
		if d.escWait() {
			return keyEvent("esc"), 1
		}
		return nil, 0
	}
	switch b[0] {
	case 0x1b:
		return d.decodeEscape()
	case '\r':
		return keyEvent("enter"), 1
	case '\n':
		// Plain \n is rare on a raw tty (LF mode off); treat as enter.
		return keyEvent("enter"), 1
	case '\t':
		return keyEvent("tab"), 1
	case 0x7f, 0x08:
		return keyEvent("backspace"), 1
	case 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x0b, 0x0c, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a:
		code := string(rune('a' + int(b[0]) - 1))
		return keyEvent("ctrl+" + code), 1
	case 0x1c:
		return keyEvent("ctrl+\\"), 1
	case 0x1d:
		return keyEvent("ctrl+]"), 1
	case 0x1e:
		return keyEvent("ctrl+^"), 1
	case 0x1f:
		return keyEvent("ctrl+_"), 1
	default:
		// Printable rune (multi-byte UTF-8).
		r, size := decodeRune(b)
		if size == 0 {
			// Incomplete UTF-8: wait.
			if len(b) < 4 {
				return nil, 0
			}
			return keyEventText(""), 1
		}
		return keyEventText(string(r)), size
	}
}

func (d *Decoder) escWait() bool {
	// A lone ESC with no bytes following within escDelay resolves to Esc.
	deadline := time.Now().Add(d.escDelay)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		n := len(d.buf)
		d.mu.Unlock()
		if n >= 2 {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
	d.mu.Lock()
	n := len(d.buf)
	d.mu.Unlock()
	return n < 2
}

// decodeEscape handles every 0x1b-prefixed sequence.
func (d *Decoder) decodeEscape() (*Event, int) {
	b := d.buf
	if len(b) < 2 {
		if d.escWait() {
			return keyEvent("esc"), 1
		}
		return nil, 0
	}
	switch b[1] {
	case '[':
		return d.decodeCSI()
	case 'O':
		if len(b) < 3 {
			return nil, 0
		}
		switch b[2] {
		case 'A':
			return keyEvent("up"), 3
		case 'B':
			return keyEvent("down"), 3
		case 'C':
			return keyEvent("right"), 3
		case 'D':
			return keyEvent("left"), 3
		case 'H':
			return keyEvent("home"), 3
		case 'F':
			return keyEvent("end"), 3
		case 'P':
			return keyEvent("f1"), 3
		case 'Q':
			return keyEvent("f2"), 3
		case 'R':
			return keyEvent("f3"), 3
		case 'S':
			return keyEvent("f4"), 3
		}
		return keyEvent("esc"), 1
	default:
		// Alt+key: ESC followed by a printable byte.
		r, size := decodeRune(b[1:])
		if size == 0 {
			return keyEvent("esc"), 1
		}
		if r >= 'a' && r <= 'z' {
			return keyEvent("alt+" + string(r)), 1 + size
		}
		if r >= 'A' && r <= 'Z' {
			return &Event{Key: &Key{Code: "alt+" + strings.ToLower(string(r)), Mods: ModAlt | ModShift, Text: string(r)}}, 1 + size
		}
		if r == '\r' {
			return keyEvent("alt+enter"), 1 + size
		}
		return &Event{Key: &Key{Code: "alt+" + string(r), Mods: ModAlt, Text: string(r)}}, 1 + size
	}
}

// decodeCSI parses \x1b[ ... final-byte sequences.
func (d *Decoder) decodeCSI() (*Event, int) {
	b := d.buf
	// Find the final byte in 0x40-0x7e.
	i := 2
	for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
		i++
	}
	if i >= len(b) {
		return nil, 0
	}
	final := b[i]
	params := string(b[2:i])
	used := i + 1

	fields := strings.Split(params, ";")
	nums := make([]int, len(fields))
	for idx, f := range fields {
		if f == "" {
			continue
		}
		v, err := strconv.Atoi(f)
		if err == nil {
			nums[idx] = v
		}
	}
	mod := func() Mods {
		if len(nums) > 1 {
			return csiMods(nums[1])
		}
		return 0
	}

	switch final {
	case 'A':
		return modKey("up", mod()), used
	case 'B':
		return modKey("down", mod()), used
	case 'C':
		return modKey("right", mod()), used
	case 'D':
		return modKey("left", mod()), used
	case 'H':
		return modKey("home", mod()), used
	case 'F':
		return modKey("end", mod()), used
	case 'M', 'm':
		// SGR mouse: CSI < b ; x ; y (M press/move, m release)
		if len(params) > 0 && params[0] == '<' {
			trimmed := strings.Split(params[1:], ";")
			mouseNums := make([]int, len(trimmed))
			for idx, f := range trimmed {
				if v, err := strconv.Atoi(f); err == nil {
					mouseNums[idx] = v
				}
			}
			return d.decodeMouse(mouseNums, final == 'm'), used
		}
		return nil, used
	case 'I':
		return &Event{Focus: &Focus{Gained: true}}, used
	case 'O':
		return &Event{Focus: &Focus{Gained: false}}, used
	case '~':
		if len(nums) == 0 {
			return nil, used
		}
		switch nums[0] {
		case 200:
			// Bracketed paste start: gather until 201~.
			start := used
			endIdx := -1
			endUsed := -1
			for j := start; j+1 < len(b); j++ {
				if b[j] == 0x1b && b[j+1] == '[' {
					// find terminator
					k := j + 2
					for k < len(b) && (b[k] < 0x40 || b[k] > 0x7e) {
						k++
					}
					if k < len(b) && b[k] == '~' {
						if string(b[j+2:k]) == "201" {
							endIdx = j
							endUsed = k + 1
						}
						break
					}
				}
			}
			if endIdx < 0 {
				return nil, 0
			}
			text := strings.ReplaceAll(string(b[start:endIdx]), "\r\n", "\n")
			text = strings.ReplaceAll(text, "\r", "\n")
			return &Event{Paste: &Paste{Text: text}}, endUsed
		case 1, 7:
			return modKey("home", mod()), used
		case 2:
			return keyEvent("insert"), used
		case 3:
			if m := mod(); m&ModShift != 0 {
				return keyEvent("shift+delete"), used
			}
			return keyEvent("delete"), used
		case 4, 8:
			return modKey("end", mod()), used
		case 5:
			return modKey("pgup", mod()), used
		case 6:
			return modKey("pgdown", mod()), used
		case 11:
			return keyEvent("f1"), used
		case 12:
			return keyEvent("f2"), used
		case 13:
			return keyEvent("f3"), used
		case 14:
			return keyEvent("f4"), used
		case 27:
			// modifyOtherKeys / xterm CSI 27;mod;code~
			if len(nums) >= 3 {
				return decodeModifyOther(nums[1], nums[2]), used
			}
		}
		// Kitty-style CSI u encoded as CSI code;mods u is handled by 'u'.
		return nil, used
	case 'u':
		// Kitty keyboard protocol: CSI codepoint;mods u
		if len(nums) >= 1 {
			code := nums[0]
			ev := decodeKitty(code, mod())
			if ev != nil {
				return ev, used
			}
		}
		return nil, used
	}
	// Unknown: swallow it.
	return nil, used
}

// decodeMouse decodes SGR mouse coordinates into a Mouse event.
func (d *Decoder) decodeMouse(nums []int, release bool) *Event {
	if len(nums) < 3 {
		return nil
	}
	btn := nums[0]
	x, y := nums[1]-1, nums[2]-1
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	const wheelFlag = 64
	const dragFlag = 32
	wrap := func(m Mouse) *Event { return &Event{Mouse: &m} }
	switch {
	case btn&wheelFlag != 0:
		if btn&1 != 0 {
			return wrap(Mouse{X: x, Y: y, Action: MouseWheelDown})
		}
		return wrap(Mouse{X: x, Y: y, Action: MouseWheelUp})
	case btn&dragFlag != 0:
		return wrap(Mouse{X: x, Y: y, Button: btn & 3, Action: MouseDrag})
	case release:
		return wrap(Mouse{X: x, Y: y, Button: btn & 3, Action: MouseRelease})
	default:
		return wrap(Mouse{X: x, Y: y, Button: btn & 3, Action: MousePress})
	}
}

// csiMods maps a CSI modifier parameter onto Mods. The wire value is
// modifier+1 (1 = none, 2 = shift, 3 = alt, 5 = ctrl, …).
func csiMods(m int) Mods {
	if m < 2 {
		return 0
	}
	v := m - 1
	var out Mods
	if v&1 != 0 {
		out |= ModShift
	}
	if v&2 != 0 {
		out |= ModAlt
	}
	if v&4 != 0 {
		out |= ModCtrl
	}
	return out
}

// decodeKitty maps kitty CSI-u codepoints onto canonical keys.
func decodeKitty(code int, m Mods) *Event {
	switch code {
	case 13:
		if m&ModShift != 0 {
			return &Event{Key: &Key{Code: "shift+enter", Mods: m}}
		}
		if m&ModCtrl != 0 {
			return &Event{Key: &Key{Code: "ctrl+enter", Mods: m}}
		}
		if m&ModAlt != 0 {
			return &Event{Key: &Key{Code: "alt+enter", Mods: m}}
		}
		return keyEvent("enter")
	case 9:
		return modKey("tab", m)
	case 27:
		return keyEvent("esc")
	case 127:
		return modKey("backspace", m)
	}
	if code >= 'a' && code <= 'z' && m&ModCtrl != 0 {
		name := "ctrl+" + string(rune(code))
		if m&ModAlt != 0 {
			name = "alt+" + name
		}
		if m&ModShift != 0 {
			name = "shift+" + name
		}
		return &Event{Key: &Key{Code: name, Mods: m}}
	}
	return nil
}

// decodeModifyOtherKeys maps xterm's CSI 27;mod;code~ form.
func decodeModifyOther(mod int, code int) *Event {
	m := csiMods(mod)
	switch code {
	case 13:
		if m&ModShift != 0 {
			return &Event{Key: &Key{Code: "shift+enter", Mods: m}}
		}
		if m&ModCtrl != 0 {
			return &Event{Key: &Key{Code: "ctrl+enter", Mods: m}}
		}
		if m&ModAlt != 0 {
			return &Event{Key: &Key{Code: "alt+enter", Mods: m}}
		}
		return keyEvent("enter")
	}
	if code >= 'a' && code <= 'z' && m&ModCtrl != 0 {
		return &Event{Key: &Key{Code: "ctrl+" + string(rune(code)), Mods: m}}
	}
	return nil
}

func keyEvent(code string) *Event  { return &Event{Key: &Key{Code: code}} }
func keyEventText(s string) *Event { return &Event{Key: &Key{Code: "rune", Text: s}} }
func modKey(code string, m Mods) *Event {
	if m == 0 {
		return keyEvent(code)
	}
	return &Event{Key: &Key{Code: code, Mods: m}}
}

// decodeRune decodes one UTF-8 rune, returning 0 width when incomplete.
func decodeRune(b []byte) (rune, int) {
	if len(b) == 0 {
		return 0, 0
	}
	c := b[0]
	switch {
	case c < 0x80:
		return rune(c), 1
	case c&0xe0 == 0xc0:
		if len(b) < 2 {
			return 0, 0
		}
		return rune(c&0x1f)<<6 | rune(b[1]&0x3f), 2
	case c&0xf0 == 0xe0:
		if len(b) < 3 {
			return 0, 0
		}
		return rune(c&0x0f)<<12 | rune(b[1]&0x3f)<<6 | rune(b[2]&0x3f), 3
	case c&0xf8 == 0xf0:
		if len(b) < 4 {
			return 0, 0
		}
		return rune(c&0x07)<<18 | rune(b[1]&0x3f)<<12 | rune(b[2]&0x3f)<<6 | rune(b[3]&0x3f), 4
	}
	return 0, 1
}
