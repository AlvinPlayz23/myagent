// Command mouseprobe is the S0 manual probe for bubbletea v2 mouse delivery:
// clicks, wheel buttons, and all-motion events in alt-screen mode.
//
// Run from the repo root: go run ./spikes/gotui-s0/mouseprobe
// It writes mouseprobe.log in the working directory and quits on q/esc/ctrl+c.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type tickMsg time.Time

type model struct {
	log                    *os.File
	width, height          int
	clicks, wheels, motion int
	rate                   int
	recent                 []time.Time
	lines                  []string
	lastSecond             int64
}

func (m *model) Init() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) logf(format string, args ...any) {
	line := time.Now().Format("15:04:05.000") + " " + fmt.Sprintf(format, args...)
	fmt.Fprintln(m.log, line)
	m.lines = append(m.lines, line)
	if len(m.lines) > 100 {
		m.lines = m.lines[len(m.lines)-100:]
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	case tea.MouseClickMsg:
		m.clicks++
		m.logf("click x=%d y=%d button=%s", msg.X, msg.Y, msg.Button)
	case tea.MouseWheelMsg:
		m.wheels++
		delta := 0
		if msg.Button == tea.MouseWheelUp || msg.Button == tea.MouseWheelLeft {
			delta = -1
		} else if msg.Button == tea.MouseWheelDown || msg.Button == tea.MouseWheelRight {
			delta = 1
		}
		m.logf("wheel x=%d y=%d button=%s delta=%+d", msg.X, msg.Y, msg.Button, delta)
	case tea.MouseMotionMsg:
		m.motion++
		now := time.Now()
		m.recent = append(m.recent, now)
		for len(m.recent) > 0 && now.Sub(m.recent[0]) > time.Second {
			m.recent = m.recent[1:]
		}
	case tickMsg:
		now := time.Time(msg)
		for len(m.recent) > 0 && now.Sub(m.recent[0]) > time.Second {
			m.recent = m.recent[1:]
		}
		m.rate = len(m.recent)
		if sec := now.Unix(); sec != m.lastSecond {
			m.lastSecond = sec
			m.logf("motion rate=%d/s total=%d", m.rate, m.motion)
		}
		return m, tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
	}
	return m, nil
}

func (m *model) View() tea.View {
	w := m.width
	if w <= 0 {
		w = 80
	}
	status := fmt.Sprintf("motion %d/s  clicks %d  wheel %d", m.rate, m.clicks, m.wheels)
	if len(status) > w {
		status = status[len(status)-w:]
	}
	body := []string{
		strings.Repeat(" ", w-len(status)) + status,
		"",
		"mouseprobe: click, wheel, move. q/esc/ctrl+c quits. log: mouseprobe.log",
		"",
	}
	start := len(m.lines) - 10
	if start < 0 {
		start = 0
	}
	body = append(body, m.lines[start:]...)
	v := tea.NewView(strings.Join(body, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func main() {
	f, err := os.Create("mouseprobe.log")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	fmt.Fprintln(f, "# mouseprobe: bubbletea v2.0.8 alt-screen MouseModeAllMotion")

	m := &model{log: f}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	fmt.Fprintf(f, "# summary clicks=%d wheels=%d motion=%d\n", m.clicks, m.wheels, m.motion)
	fmt.Println("wrote mouseprobe.log")
}
