package main

import (
	"bufio"
	"fmt"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/timer"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wordwrap"
	mac "github.com/villadelfia/multi-ai-client"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	aKey, oKey, mKey := loadKeys()
	if aKey == "" && oKey == "" && mKey == "" {
		panic("Please add your keys to keys.conf.")
	}

	p := tea.NewProgram(createModel(aKey, oKey, mKey), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}

type model struct {
	client      *mac.Client
	input       textarea.Model
	instruction string
	view        viewport.Model
	timer       timer.Model

	state        *int
	systemPrompt string
	options      *[]string
	ready        bool
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.timer.Init())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var (
		iCmd tea.Cmd
		vCmd tea.Cmd
		tCmd tea.Cmd
	)

	m.input, iCmd = m.input.Update(msg)
	m.view, vCmd = m.view.Update(msg)
	cmds = append(cmds, iCmd, vCmd)

	switch msg := msg.(type) {
	case timer.TimeoutMsg, timer.StartStopMsg:
		m.timer, tCmd = m.timer.Update(msg)
		cmds = append(cmds, tCmd)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if !m.ready {
			m.view = viewport.New(msg.Width, msg.Height-12)
			m.view.SetContent("")
			m.ready = true
		} else {
			m.view.Width = msg.Width
			m.view.Height = msg.Height - 12
		}
		m.input.SetWidth(msg.Width)
		m.input.SetHeight(8)
	}

	if *m.state == -1 {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			}
		}
	} else if *m.state == 0 {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEnter:
				m.systemPrompt = strings.TrimSpace(m.input.Value())
				m.input.Reset()
				if m.systemPrompt != "" {
					m.client.Chat.SetSystemMessage(m.systemPrompt)
				}
				m.client.Chat.SetSystemMessage(m.systemPrompt)
				*m.state = 1
				m.instruction = "Enter a message and press Enter to send it. Press Ctrl+C to quit."
			}
		}
	} else if *m.state == 1 {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEnter:
				i, ch, _ := m.client.CreateResponseWithPrompt(strings.TrimSpace(m.input.Value()), "")

				m.input.Reset()
				m.instruction = "Wait..."
				*m.state = -1
				options := make([]string, i)
				m.options = &options

				go func(i int, ch chan mac.MessageChunk, options *[]string) {
					for chunk := range ch {
						(*options)[chunk.Index] += chunk.Delta
					}
					*m.state = 2
				}(i, ch, m.options)
			}
		}
	} else if *m.state == 2 {
		m.input.Reset()
		m.instruction = "Choose an option by typing its number and pressing Enter, typing anything else will regenerate the reply. Press Ctrl+C to quit."
		*m.state = 3
	} else if *m.state == 3 {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEnter:
				val := strings.TrimSpace(m.input.Value())
				num, err := strconv.Atoi(val)
				if err != nil || num < 0 || num >= len(*m.options) {
					i, ch, _ := m.client.CreateResponseWithPrompt(strings.TrimSpace(m.input.Value()), "")

					m.input.Reset()
					m.instruction = "Wait..."
					*m.state = -1
					options := make([]string, i)
					m.options = &options

					go func(i int, ch chan mac.MessageChunk, options *[]string) {
						for chunk := range ch {
							(*options)[chunk.Index] += chunk.Delta
						}
						*m.state = 2
					}(i, ch, m.options)
				} else {
					m.client.Chat.AddAssistantMessage((*m.options)[num])
					m.input.Reset()
					m.instruction = "Enter a message and press Enter to send it. Press Ctrl+C to quit."
					*m.state = 1
				}
			}
		}
	}

	if m.options != nil && *m.state != 1 {
		disp := m.client.String() + "\n"
		for i, r := range *m.options {
			disp += fmt.Sprintf("\nResponse %d: %s\n", i, r)
		}
		m.view.SetContent(wordwrap.String(disp, m.view.Width))
		m.view.GotoBottom()
	} else {
		m.view.SetContent(wordwrap.String(m.client.String(), m.view.Width))
		m.view.GotoBottom()
	}

	if m.timer.Running() == false {
		if m.timer.Timedout() {
			cmds = append(cmds, m.timer.Init())
		} else {
			cmds = append(cmds, m.timer.Start())
		}
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "Loading..."
	}
	return fmt.Sprintf(
		"%s\n\n%s\n\n%s",
		m.view.View(),
		m.instruction,
		m.input.View(),
	) + "\n\n"
}

func createModel(aKey, oKey, mKey string) model {
	// Create the client.
	client := &mac.Client{}
	claude := mac.NewModelDefinition("Claude", mac.Anthropic, aKey, "claude-3-opus-20240229")
	gpt := mac.NewModelDefinition("GPT4", mac.OpenAI, oKey, "gpt-4-turbo-preview")
	mistral := mac.NewModelDefinition("Mistral Large", mac.Mistral, mKey, "mistral-large-latest")
	client.AddModelDefinition(claude)
	client.AddModelDefinition(gpt)
	client.AddModelDefinition(mistral)

	input := textarea.New()
	input.Placeholder = ""
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.SetHeight(4)
	input.SetWidth(120)
	input.Focus()

	input.KeyMap.InsertNewline.SetEnabled(false)

	return model{
		client:      client,
		input:       input,
		instruction: "Enter a system message (or leave empty) and press Enter to set it. Press Ctrl+C to quit.",
		timer:       timer.NewWithInterval(1000*time.Hour, 500*time.Millisecond),
		state:       new(int),
	}
}

func loadKeys() (string, string, string) {
	file, err := os.Open("keys.conf")
	if err != nil {
		return "", "", ""
	}
	defer file.Close()

	anthropicKey := ""
	openAiKey := ""
	mistralKey := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "ANTHROPIC_API_KEY=") {
			anthropicKey = strings.TrimSuffix(strings.TrimPrefix(line, "ANTHROPIC_API_KEY=\""), "\"")
		}
		if strings.HasPrefix(line, "OPENAI_API_KEY=") {
			openAiKey = strings.TrimSuffix(strings.TrimPrefix(line, "OPENAI_API_KEY=\""), "\"")
		}
		if strings.HasPrefix(line, "MISTRAL_API_KEY=") {
			mistralKey = strings.TrimSuffix(strings.TrimPrefix(line, "MISTRAL_API_KEY=\""), "\"")
		}
	}
	return anthropicKey, openAiKey, mistralKey
}
