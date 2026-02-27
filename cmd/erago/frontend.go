package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	eruntime "github.com/gosuda/erago/runtime"
)

type model struct {
	cfg      appConfig
	viewport viewport.Model
	input    textinput.Model
	ready    bool
	width    int
	height   int
	status   string
	running  bool
	events   <-chan tea.Msg
	pending  *pendingInput
	seq      int
	history  []string
	tail     string
	stream   []eruntime.Output
}

var (
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	inputStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("24")).Padding(0, 1)
)

func newModel(cfg appConfig) model {
	vp := viewport.New(
		viewport.WithWidth(80),
		viewport.WithHeight(20),
	)
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 4096
	ti.SetValue("")
	return model{
		cfg:      cfg,
		viewport: vp,
		input:    ti,
		status:   "starting",
		history:  nil,
		tail:     "",
		stream:   nil,
	}
}

func startVM(cfg appConfig) tea.Cmd {
	return func() tea.Msg {
		events := make(chan tea.Msg, 256)
		go runVM(cfg, events)
		return vmStartedMsg{events: events}
	}
}

func waitVMEvent(events <-chan tea.Msg) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case msg, ok := <-events:
			if !ok {
				return nil
			}
			return msg
		case <-time.After(20 * time.Millisecond):
			return vmPollMsg{}
		}
	}
}

func sendInputResp(ch chan vmInputResp, resp vmInputResp) {
	select {
	case ch <- resp:
		return
	default:
	}
	// If a stale response is buffered, replace it with the latest response.
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- resp:
	default:
	}
}

func isEnterKey(msg tea.KeyMsg) bool {
	k := msg.Key()
	if k.Code == tea.KeyEnter || k.Code == tea.KeyKpEnter {
		return true
	}
	switch msg.String() {
	case "enter", "ctrl+m":
		return true
	default:
		return false
	}
}

func timeoutCmd(seq int, d time.Duration) tea.Cmd {
	if d <= 0 {
		return func() tea.Msg { return vmTimeoutMsg{seq: seq} }
	}
	return tea.Tick(d, func(time.Time) tea.Msg {
		return vmTimeoutMsg{seq: seq}
	})
}

func countdownCmd(seq int) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return vmCountdownMsg{seq: seq}
	})
}

func ceilSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64((d + time.Second - 1) / time.Second)
}

func firstRuneText(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[0])
}

func isWaitRequest(req eruntime.InputRequest) bool {
	if req.Numeric {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(req.Command)) {
	case "WAIT", "WAITANYKEY", "FORCEWAIT", "TWAIT", "AWAIT", "INPUTANY":
		return true
	default:
		return false
	}
}

func (m model) Init() tea.Cmd {
	return startVM(m.cfg)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		footerLines := 3
		if m.pending != nil {
			footerLines++
			if !m.pending.isWait {
				footerLines++
			}
		}
		vh := msg.Height - footerLines
		if vh < 1 {
			vh = 1
		}
		m.viewport.SetWidth(msg.Width)
		m.viewport.SetHeight(vh)
		m.ready = true
		return m, nil

	case vmStartedMsg:
		m.events = msg.events
		m.running = true
		m.status = "running"
		return m, waitVMEvent(m.events)

	case vmOutputMsg:
		m.appendOutput(msg.out)
		return m, waitVMEvent(m.events)

	case vmPollMsg:
		if m.running && m.pending == nil {
			return m, waitVMEvent(m.events)
		}
		return m, nil

	case vmPromptMsg:
		m.seq++
		m.pending = &pendingInput{
			req:      msg.req,
			resp:     msg.resp,
			seq:      m.seq,
			isWait:   isWaitRequest(msg.req),
			deadline: time.Now().Add(time.Duration(msg.req.TimeoutMs) * time.Millisecond),
		}
		m.input.SetValue("")
		m.input.Placeholder = ""
		if m.pending.isWait {
			m.input.Blur()
		} else {
			m.input.Focus()
		}
		m.setPromptStatus()
		if msg.req.Timed && msg.req.Countdown {
			return m, tea.Batch(
				timeoutCmd(m.pending.seq, time.Duration(msg.req.TimeoutMs)*time.Millisecond),
				countdownCmd(m.pending.seq),
			)
		}
		if msg.req.Timed {
			return m, timeoutCmd(m.pending.seq, time.Duration(msg.req.TimeoutMs)*time.Millisecond)
		}
		return m, nil

	case vmCountdownMsg:
		if m.pending != nil && m.pending.seq == msg.seq && m.pending.req.Timed && m.pending.req.Countdown {
			m.setPromptStatus()
			return m, countdownCmd(msg.seq)
		}
		return m, nil

	case vmTimeoutMsg:
		if m.pending != nil && m.pending.seq == msg.seq && m.pending.req.Timed {
			sendInputResp(m.pending.resp, vmInputResp{value: "", timeout: true})
			m.pending = nil
			m.input.Blur()
			m.status = "running"
			return m, waitVMEvent(m.events)
		}
		return m, nil

	case vmDoneMsg:
		m.running = false
		m.pending = nil
		m.input.Blur()
		if msg.err != nil {
			m.status = "failed"
			m.appendOutput(eruntime.Output{Text: errStyle.Render(msg.err.Error()), NewLine: true})
		} else {
			m.status = "done"
		}
		return m, nil

	case tea.KeyMsg:
		k := msg.Key()
		if ((k.Code == 'c' || k.Code == 'C') && k.Mod == tea.ModCtrl) || msg.String() == "ctrl+c" {
			if m.pending != nil {
				sendInputResp(m.pending.resp, vmInputResp{value: "", timeout: true})
			}
			return m, tea.Quit
		}

		if m.pending != nil {
			if m.pending.isWait {
				sendInputResp(m.pending.resp, vmInputResp{value: "", timeout: false})
				m.pending = nil
				m.input.Blur()
				m.status = "running"
				return m, waitVMEvent(m.events)
			}
			if m.pending.req.OneInput && msg.Key().Text != "" {
				val := firstRuneText(msg.Key().Text)
				sendInputResp(m.pending.resp, vmInputResp{value: val, timeout: false})
				m.pending = nil
				m.input.Blur()
				m.input.SetValue("")
				m.status = "running"
				return m, waitVMEvent(m.events)
			}
			if isEnterKey(msg) {
				val := strings.TrimSpace(m.input.Value())
				if !m.pending.req.Numeric && m.pending.req.OneInput {
					val = firstRuneText(val)
				}
				sendInputResp(m.pending.resp, vmInputResp{value: val, timeout: false})
				m.pending = nil
				m.input.Blur()
				m.input.SetValue("")
				m.status = "running"
				return m, waitVMEvent(m.events)
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "r":
			if m.running {
				return m, nil
			}
			m.clearForRestart()
			m.status = "restarting"
			return m, startVM(m.cfg)
		case "g", "home":
			m.viewport.GotoTop()
			return m, nil
		case "G", "end":
			m.viewport.GotoBottom()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m model) View() tea.View {
	if !m.ready {
		v := tea.NewView("initializing...")
		v.AltScreen = true
		return v
	}
	parts := []string{m.viewport.View()}
	if m.pending != nil {
		if !m.pending.isWait {
			parts = append(parts, inputStyle.Render(m.input.View()))
		}
	}
	v := tea.NewView(strings.Join(parts, "\n"))
	v.AltScreen = true
	return v
}

func (m *model) appendOutput(out eruntime.Output) {
	if out.ClearLines > 0 {
		n := out.ClearLines
		if n > len(m.stream) {
			n = len(m.stream)
		}
		m.stream = m.stream[:len(m.stream)-n]
	} else {
		m.stream = append(m.stream, out)
	}
	m.rebuildContent()
}

func (m *model) rebuildContent() {
	m.history = m.history[:0]
	m.tail = ""
	for _, out := range m.stream {
		if out.NewLine {
			m.history = append(m.history, m.tail+out.Text)
			m.tail = ""
		} else {
			m.tail += out.Text
		}
	}
	content := strings.Join(m.history, "\n")
	if m.tail != "" {
		if content != "" {
			content += "\n"
		}
		content += m.tail
	}
	if content == "" {
		content = "(no output yet)"
	}
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m *model) clearForRestart() {
	m.history = nil
	m.tail = ""
	m.stream = nil
	m.viewport.SetContent("")
	m.pending = nil
	m.seq = 0
	m.input.Blur()
	m.input.SetValue("")
}

func (m *model) setPromptStatus() {
	if m.pending == nil {
		return
	}
	cmd := strings.ToUpper(strings.TrimSpace(m.pending.req.Command))
	if m.pending.req.Timed && m.pending.req.TimeoutMs > 0 {
		if m.pending.req.Countdown {
			sec := ceilSeconds(time.Until(m.pending.deadline))
			if sec < 0 {
				sec = 0
			}
			if m.pending.isWait {
				m.status = fmt.Sprintf("%s: %ds (key wait)", cmd, sec)
				return
			}
			m.status = fmt.Sprintf("%s: %ds (input wait)", cmd, sec)
			return
		}
		if m.pending.isWait {
			m.status = fmt.Sprintf("%s: timed key wait", cmd)
			return
		}
		m.status = fmt.Sprintf("%s: timed input wait", cmd)
		return
	}
	if m.pending.isWait {
		m.status = fmt.Sprintf("%s: key wait", cmd)
		return
	}
	m.status = fmt.Sprintf("%s: input wait", cmd)
}
