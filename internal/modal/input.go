package modal

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcus/sidecar/internal/styles"
)

// --- Input Section ---

// InputOption is a functional option for Input sections.
type InputOption func(*inputSection)

// inputSection wraps a bubbles textinput.Model.
type inputSection struct {
	id            string
	label         string
	model         *textinput.Model
	submitOnEnter bool
	submitAction  string // Action ID to return on submit (defaults to modal primaryAction)
}

// Input creates an input section wrapping a textinput.Model.
func Input(id string, model *textinput.Model, opts ...InputOption) Section {
	s := &inputSection{
		id:            id,
		model:         model,
		submitOnEnter: true, // Default to submit on enter
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// InputWithLabel creates an input section with a label.
func InputWithLabel(id, label string, model *textinput.Model, opts ...InputOption) Section {
	s := &inputSection{
		id:            id,
		label:         label,
		model:         model,
		submitOnEnter: true,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithSubmitOnEnter enables or disables submit-on-enter behavior.
func WithSubmitOnEnter(submit bool) InputOption {
	return func(s *inputSection) {
		s.submitOnEnter = submit
	}
}

// WithSubmitAction sets the action ID returned on submit.
func WithSubmitAction(actionID string) InputOption {
	return func(s *inputSection) {
		s.submitAction = actionID
	}
}

func (s *inputSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	isFocused := s.id == focusID

	var sb strings.Builder
	labelLines := 0

	// Render label if present
	if s.label != "" {
		sb.WriteString(styles.Body.Render(s.label))
		sb.WriteString("\n")
		labelLines = 1
	}

	// Update model width and focus state
	inputInnerWidth := contentWidth - 4
	if inputInnerWidth < 1 {
		inputInnerWidth = 1
	}
	inputBoxWidth := contentWidth - 2
	if inputBoxWidth < 1 {
		inputBoxWidth = 1
	}

	if s.model != nil {
		s.model.Width = inputInnerWidth // Account for input padding/border
		if isFocused {
			s.model.Focus()
		} else {
			s.model.Blur()
		}
	}

	// Determine input style based on focus/hover
	var inputStyle lipgloss.Style
	if isFocused {
		inputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(styles.Primary).
			Width(inputBoxWidth)
	} else if s.id == hoverID {
		inputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(styles.TextMuted).
			Width(inputBoxWidth)
	} else {
		inputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(styles.BorderNormal).
			Width(inputBoxWidth)
	}

	// Render input
	inputView := ""
	if s.model != nil {
		inputView = s.model.View()
	}
	renderedInput := inputStyle.Render(inputView)
	sb.WriteString(renderedInput)

	content := sb.String()
	inputHeight := lipgloss.Height(renderedInput)

	return RenderedSection{
		Content: content,
		Focusables: []FocusableInfo{{
			ID:      s.id,
			OffsetX: 0,
			OffsetY: labelLines,
			Width:   contentWidth,
			Height:  inputHeight,
		}},
	}
}

func (s *inputSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if s.id != focusID || s.model == nil {
		return "", nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		// Forward non-key messages to the model
		var cmd tea.Cmd
		*s.model, cmd = s.model.Update(msg)
		return "", cmd
	}

	// Handle enter for submit
	if keyMsg.String() == "enter" && s.submitOnEnter {
		if s.submitAction != "" {
			return s.submitAction, nil
		}
		// Return empty string to signal the modal to use primaryAction
		return "", nil
	}

	// Forward to textinput model
	var cmd tea.Cmd
	*s.model, cmd = s.model.Update(msg)
	return "", cmd
}

// --- Textarea Section ---

// TextareaOption is a functional option for Textarea sections.
type TextareaOption func(*textareaSection)

// textareaSection wraps a bubbles textarea.Model.
type textareaSection struct {
	id     string
	label  string
	model  *textarea.Model
	height int // Desired height in lines
}

// Textarea creates a textarea section wrapping a textarea.Model.
func Textarea(id string, model *textarea.Model, height int, opts ...TextareaOption) Section {
	s := &textareaSection{
		id:     id,
		model:  model,
		height: height,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// TextareaWithLabel creates a textarea section with a label.
func TextareaWithLabel(id, label string, model *textarea.Model, height int, opts ...TextareaOption) Section {
	s := &textareaSection{
		id:     id,
		label:  label,
		model:  model,
		height: height,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *textareaSection) Render(contentWidth int, focusID, hoverID string) RenderedSection {
	isFocused := s.id == focusID

	var sb strings.Builder
	labelLines := 0

	// Render label if present
	if s.label != "" {
		sb.WriteString(styles.Body.Render(s.label))
		sb.WriteString("\n")
		labelLines = 1
	}

	// Update model dimensions and focus state
	textareaInnerWidth := contentWidth - 4
	if textareaInnerWidth < 1 {
		textareaInnerWidth = 1
	}
	textareaBoxWidth := contentWidth - 2
	if textareaBoxWidth < 1 {
		textareaBoxWidth = 1
	}

	if s.model != nil {
		s.model.SetWidth(textareaInnerWidth)
		s.model.SetHeight(s.height)
		if isFocused {
			s.model.Focus()
		} else {
			s.model.Blur()
		}
	}

	// Determine textarea style based on focus/hover
	var areaStyle lipgloss.Style
	if isFocused {
		areaStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(styles.Primary).
			Width(textareaBoxWidth)
	} else if s.id == hoverID {
		areaStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(styles.TextMuted).
			Width(textareaBoxWidth)
	} else {
		areaStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(styles.BorderNormal).
			Width(textareaBoxWidth)
	}

	// Render textarea
	areaView := ""
	if s.model != nil {
		areaView = s.model.View()
	}
	sb.WriteString(areaStyle.Render(areaView))

	content := sb.String()
	// Calculate actual rendered height
	renderedHeight := lipgloss.Height(content)

	return RenderedSection{
		Content: content,
		Focusables: []FocusableInfo{{
			ID:      s.id,
			OffsetX: 0,
			OffsetY: labelLines,
			Width:   contentWidth,
			Height:  renderedHeight - labelLines,
		}},
	}
}

func (s *textareaSection) Update(msg tea.Msg, focusID string) (string, tea.Cmd) {
	if s.id != focusID || s.model == nil {
		return "", nil
	}

	// Textareas always treat Enter as newline, never submit
	var cmd tea.Cmd
	*s.model, cmd = s.model.Update(msg)
	return "", cmd
}
