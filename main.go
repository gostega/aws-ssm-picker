package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// No hardcoded default — each developer sets their own AWS_PROFILE.

var (
	instanceIDRe  = regexp.MustCompile(`^i-[A-Za-z0-9]{8,17}$`)
	aliasLineRe   = regexp.MustCompile(`ALIASES\[([^\]]+)\]\s*=\s*"?([^"'\n]+?)"?\s*$`)
	profileLineRe = regexp.MustCompile(`^\[profile\s+(.+)\]$`)
)

// ─── Styles ───────────────────────────────────────────────────────────────────

var (
	clrOrange = lipgloss.Color("#FF6B6B")
	clrTeal   = lipgloss.Color("#4ECDC4")
	clrYellow = lipgloss.Color("#FFE66D")
	clrMuted  = lipgloss.Color("#555555")
	clrMuted2 = lipgloss.Color("#888888")
	clrBorder = lipgloss.Color("#333333")
	clrError  = lipgloss.Color("#FF4444")
	clrGreen  = lipgloss.Color("#44CC88")

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(clrOrange)
	metaStyle    = lipgloss.NewStyle().Foreground(clrMuted2)
	spinnerStyle = lipgloss.NewStyle().Foreground(clrOrange)
	loadingStyle = lipgloss.NewStyle().Foreground(clrMuted2).Italic(true)
	labelStyle   = lipgloss.NewStyle().Bold(true).Foreground(clrYellow)
	valueStyle   = lipgloss.NewStyle().Foreground(clrTeal)
	mutedStyle   = lipgloss.NewStyle().Foreground(clrMuted2)
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(clrError)
	successStyle = lipgloss.NewStyle().Foreground(clrGreen)
	helpStyle    = lipgloss.NewStyle().Foreground(clrMuted)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrBorder).
			Padding(0, 2)

	dividerStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(clrBorder)

	listTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(clrTeal).PaddingLeft(1)

	// Compact single-line instance list delegate
	rowCursorStyle = lipgloss.NewStyle().Foreground(clrOrange).Bold(true)
	rowSelName     = lipgloss.NewStyle().Foreground(clrOrange).Bold(true)
	rowSelID       = lipgloss.NewStyle().Foreground(clrMuted2)
	rowSelType     = lipgloss.NewStyle().Foreground(clrTeal)
	rowNormName    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#111111", Dark: "#dddddd"})
	rowNormID      = lipgloss.NewStyle().Foreground(clrMuted)
	rowNormType    = lipgloss.NewStyle().Foreground(clrMuted)
	rowSep         = lipgloss.NewStyle().Foreground(clrBorder).Render("  ·  ")

	// Profile picker specific
	profSelStyle  = lipgloss.NewStyle().Foreground(clrOrange).Bold(true)
	profNormStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#cccccc"})
	profCursorStr = lipgloss.NewStyle().Foreground(clrOrange).Bold(true).Render("▶")
	profSpaceStr  = "  "
)

// renderAppHeader renders the shared title bar used across all TUI screens.
func renderAppHeader(w int, subtitle string) string {
	if w == 0 {
		w = 80
	}
	right := ""
	if subtitle != "" {
		right = "  " + metaStyle.Render(subtitle)
	}
	return dividerStyle.Width(w).Render(titleStyle.Render("⚡ AWS SSM Connect") + right)
}

// ═══════════════════════════════════════════════════════════════════════════════
// ── Profile picker ─────────────────────────────────────────────────────────────
// ═══════════════════════════════════════════════════════════════════════════════

type profilePickerModel struct {
	input    textinput.Model
	blinkCmd tea.Cmd

	profiles []string // all known profiles
	filtered []string // filtered by input text
	cursor   int      // highlighted suggestion (-1 = none)

	selected  string // set on confirmation
	cancelled bool

	width  int
	height int
}

func newProfilePickerModel(profiles []string) profilePickerModel {
	ti := textinput.New()
	ti.Placeholder = "type a profile name…"
	ti.CharLimit = 128
	ti.Width = 60
	ti.PromptStyle = labelStyle
	ti.TextStyle = valueStyle
	blinkCmd := ti.Focus()

	return profilePickerModel{
		input:    ti,
		blinkCmd: blinkCmd,
		profiles: profiles,
		filtered: profiles, // all visible until the user types
		cursor:   -1,
	}
}

func (m profilePickerModel) Init() tea.Cmd { return m.blinkCmd }

func (m profilePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit

		case "esc":
			m.cancelled = true
			return m, tea.Quit

		case "enter":
			// Prefer the highlighted suggestion, otherwise whatever is in the input
			val := strings.TrimSpace(m.input.Value())
			if m.cursor >= 0 && m.cursor < len(m.filtered) {
				val = m.filtered[m.cursor]
			}
			if val == "" {
				return m, nil // don't accept empty
			}
			m.selected = val
			return m, tea.Quit

		case "up":
			if m.cursor > 0 {
				m.cursor--
			} else if m.cursor == 0 {
				m.cursor = -1
			}
			return m, nil

		case "down":
			if len(m.filtered) > 0 && m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil

		case "tab":
			// Fill input from highlighted suggestion
			if m.cursor >= 0 && m.cursor < len(m.filtered) {
				m.input.SetValue(m.filtered[m.cursor])
				m.input.CursorEnd()
			} else if len(m.filtered) > 0 {
				m.cursor = 0
				m.input.SetValue(m.filtered[0])
				m.input.CursorEnd()
			}
			return m, nil

		default:
			// Forward all other keys (letters, backspace, etc.) to the text input
			prev := m.input.Value()
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			if m.input.Value() != prev {
				// Input changed → refilter and reset suggestion cursor
				m.filtered = filterProfiles(m.profiles, m.input.Value())
				m.cursor = -1
			}
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m profilePickerModel) View() string {
	w := m.width
	header := renderAppHeader(w, "select an AWS profile")

	// Input
	inputSection := lipgloss.JoinVertical(lipgloss.Left,
		labelStyle.Render("AWS Profile"),
		"  "+m.input.View(),
	)

	// Suggestion list (max 10 items)
	const maxSugs = 10
	var lines []string
	for i, p := range m.filtered {
		if i >= maxSugs {
			lines = append(lines, mutedStyle.Render(
				fmt.Sprintf("  … and %d more — keep typing to filter", len(m.filtered)-maxSugs),
			))
			break
		}
		if i == m.cursor {
			lines = append(lines, profCursorStr+" "+profSelStyle.Render(p))
		} else {
			lines = append(lines, profSpaceStr+profNormStyle.Render(p))
		}
	}

	var sugSection string
	switch {
	case len(lines) > 0:
		sugSection = "\n" + strings.Join(lines, "\n")
	case len(m.profiles) > 0 && m.input.Value() != "":
		sugSection = "\n" + mutedStyle.Render("  no matching profiles — press Enter to use as-is")
	case len(m.profiles) == 0:
		sugSection = "\n" + mutedStyle.Render("  no profiles found in ~/.aws/config")
	}

	help := helpStyle.Render("  ↑/↓ navigate  ·  Tab fill  ·  Enter confirm  ·  Ctrl+C cancel")

	body := lipgloss.NewStyle().PaddingLeft(2).Render(inputSection+sugSection) + "\n\n" + help

	return header + "\n\n" + body + "\n"
}

// loadProfiles reads profile names from ~/.aws/config.
func loadProfiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	f, err := os.Open(filepath.Join(home, ".aws", "config"))
	if err != nil {
		return nil
	}
	defer f.Close()

	var profiles []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := profileLineRe.FindStringSubmatch(line); len(m) == 2 {
			profiles = append(profiles, m[1])
		}
		if line == "[default]" {
			profiles = append(profiles, "default")
		}
	}
	sort.Strings(profiles)
	return profiles
}

func filterProfiles(all []string, query string) []string {
	if query == "" {
		return all
	}
	q := strings.ToLower(query)
	var out []string
	for _, p := range all {
		if strings.Contains(strings.ToLower(p), q) {
			out = append(out, p)
		}
	}
	return out
}

func runProfilePicker(profiles []string) (string, error) {
	p := tea.NewProgram(newProfilePickerModel(profiles), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	fm := final.(profilePickerModel)
	if fm.cancelled {
		return "", fmt.Errorf("cancelled")
	}
	return fm.selected, nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// ── Instance picker + reason ───────────────────────────────────────────────────
// ═══════════════════════════════════════════════════════════════════════════════

type Instance struct {
	Name, ID, Type string
}

func (i Instance) Title() string       { return i.Name }
func (i Instance) Description() string { return i.ID + "  ·  " + i.Type }
func (i Instance) FilterValue() string { return i.Name + " " + i.ID + " " + i.Type }

// compactDelegate renders each instance as a single line:
//
//	▶ name  ·  i-xxxx  ·  type
type compactDelegate struct{}

func (compactDelegate) Height() int                             { return 1 }
func (compactDelegate) Spacing() int                            { return 0 }
func (compactDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (compactDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	inst, ok := item.(Instance)
	if !ok {
		return
	}
	selected := index == m.Index()

	cursor := "  "
	var name, id, typ string
	if selected {
		cursor = rowCursorStyle.Render("▶ ")
		name = rowSelName.Render(inst.Name)
		id = rowSelID.Render(inst.ID)
		typ = rowSelType.Render(inst.Type)
	} else {
		name = rowNormName.Render(inst.Name)
		id = rowNormID.Render(inst.ID)
		typ = rowNormType.Render(inst.Type)
	}

	fmt.Fprint(w, cursor+name+rowSep+id+rowSep+typ)
}

type (
	instancesLoadedMsg  []Instance
	instanceResolvedMsg struct{ id, name string }
	errMsg              struct{ err error }
)

type appState int

const (
	stateLoading appState = iota
	statePicking
	stateReason
	stateDone
)

type model struct {
	state       appState
	spinner     spinner.Model
	list        list.Model
	reasonInput textinput.Model
	blinkCmd    tea.Cmd

	awsProfile string
	awsRegion  string

	instanceID   string
	instanceName string
	reason       string

	statusMsg string
	err       error
	width     int
	height    int
}

func newModel(profile, region, instanceID, instanceName string) model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	ti := textinput.New()
	ti.Placeholder = "press Enter to skip"
	ti.CharLimit = 256
	ti.Width = 60
	ti.PromptStyle = labelStyle
	ti.TextStyle = valueStyle

	l := list.New([]list.Item{}, compactDelegate{}, 80, 24)
	l.Title = "Select an EC2 Instance"
	l.Styles.Title = listTitleStyle
	l.Styles.HelpStyle = helpStyle
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.KeyMap.Quit.SetEnabled(false)

	state := stateLoading
	statusMsg := "Loading instances..."
	var blinkCmd tea.Cmd

	switch {
	case instanceID != "":
		state = stateReason
		blinkCmd = ti.Focus()
	case instanceName != "":
		statusMsg = fmt.Sprintf("Resolving %q...", instanceName)
	}

	return model{
		state:        state,
		spinner:      sp,
		list:         l,
		reasonInput:  ti,
		blinkCmd:     blinkCmd,
		awsProfile:   profile,
		awsRegion:    region,
		instanceID:   instanceID,
		instanceName: instanceName,
		statusMsg:    statusMsg,
	}
}

func (m model) Init() tea.Cmd {
	switch m.state {
	case stateReason:
		return m.blinkCmd
	case stateLoading:
		if m.instanceName != "" {
			return tea.Batch(m.spinner.Tick, resolveCmd(m.awsProfile, m.awsRegion, m.instanceName))
		}
		return tea.Batch(m.spinner.Tick, listInstancesCmd(m.awsProfile, m.awsRegion))
	}
	return nil
}

// ─── AWS CLI helpers ──────────────────────────────────────────────────────────

// cleanEnv strips pager/auto-prompt vars so AWS CLI calls are non-interactive.
func cleanEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "AWS_CLI_AUTO_PROMPT=") || strings.HasPrefix(e, "AWS_PAGER=") {
			continue
		}
		env = append(env, e)
	}
	return append(env, "AWS_CLI_AUTO_PROMPT=off", "AWS_PAGER=")
}

func awsCLI(args ...string) ([]byte, error) {
	cmd := exec.Command("aws", args...)
	cmd.Env = cleanEnv()
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func listInstancesCmd(profile, region string) tea.Cmd {
	return func() tea.Msg {
		out, err := awsCLI(
			"ec2", "describe-instances",
			"--profile", profile,
			"--region", region,
			"--filters", "Name=instance-state-name,Values=running",
			"--query", "Reservations[].Instances[].[Tags[?Key=='Name']|[0].Value,InstanceId,InstanceType]",
			"--output", "json",
		)
		if err != nil {
			return errMsg{fmt.Errorf("describe instances: %w", err)}
		}

		var rows [][]any
		if err := json.Unmarshal(out, &rows); err != nil {
			return errMsg{fmt.Errorf("parse response: %w", err)}
		}

		var instances []Instance
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			name, _ := row[0].(string)
			id, _ := row[1].(string)
			itype, _ := row[2].(string)
			if id == "" || id == "None" {
				continue
			}
			if name == "" || name == "None" {
				name = id
			}
			instances = append(instances, Instance{Name: name, ID: id, Type: itype})
		}

		if len(instances) == 0 {
			return errMsg{fmt.Errorf("no running instances found in region %s", region)}
		}

		sort.Slice(instances, func(i, j int) bool {
			return instances[i].Name < instances[j].Name
		})

		return instancesLoadedMsg(instances)
	}
}

func resolveCmd(profile, region, name string) tea.Cmd {
	return func() tea.Msg {
		out, err := awsCLI(
			"ec2", "describe-instances",
			"--profile", profile,
			"--region", region,
			"--filters",
			"Name=instance-state-name,Values=running",
			"Name=tag:Name,Values="+name,
			"--query", "Reservations[0].Instances[0].InstanceId",
			"--output", "text",
		)
		if err != nil {
			return errMsg{fmt.Errorf("describe instances: %w", err)}
		}

		id := strings.TrimSpace(string(out))
		if id == "" || id == "None" {
			return errMsg{fmt.Errorf("no running instance found with name %q", name)}
		}
		return instanceResolvedMsg{id: id, name: name}
	}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height - 3)
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		switch m.state {
		case statePicking:
			if m.list.FilterState() != list.Filtering {
				switch msg.String() {
				case "q", "esc":
					return m, tea.Quit
				case "enter":
					if item, ok := m.list.SelectedItem().(Instance); ok {
						m.instanceID = item.ID
						m.instanceName = item.Name
						m.state = stateReason
						return m, m.reasonInput.Focus()
					}
				}
			}

		case stateReason:
			switch msg.String() {
			case "enter":
				m.reason = strings.TrimSpace(m.reasonInput.Value())
				m.state = stateDone
				return m, tea.Quit
			case "esc":
				return m, tea.Quit
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case instancesLoadedMsg:
		items := make([]list.Item, len(msg))
		for i, inst := range msg {
			items[i] = inst
		}
		m.list.SetItems(items)
		m.state = statePicking
		return m, nil

	case instanceResolvedMsg:
		m.instanceID = msg.id
		m.instanceName = msg.name
		m.state = stateReason
		return m, m.reasonInput.Focus()

	case errMsg:
		m.err = msg.err
		return m, tea.Quit
	}

	var cmd tea.Cmd
	switch m.state {
	case statePicking:
		m.list, cmd = m.list.Update(msg)
	case stateReason:
		m.reasonInput, cmd = m.reasonInput.Update(msg)
	}
	return m, cmd
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	w := m.width
	header := renderAppHeader(w, "profile: "+m.awsProfile+"  ·  region: "+m.awsRegion)

	var body string
	switch m.state {
	case stateLoading:
		body = "\n  " + m.spinner.View() + "  " + loadingStyle.Render(m.statusMsg) + "\n"

	case statePicking:
		body = m.list.View()

	case stateReason:
		nameAndID := valueStyle.Render(m.instanceName) + "  " + mutedStyle.Render(m.instanceID)
		card := cardStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
			labelStyle.Render("Instance"),
			"  "+nameAndID,
		))

		prompt := lipgloss.JoinVertical(lipgloss.Left,
			labelStyle.Render("Login reason"),
			"  "+m.reasonInput.View(),
			"",
			helpStyle.Render("  shown in CloudTrail  ·  Enter to skip  ·  Esc / Ctrl+C to cancel"),
		)

		body = "\n" +
			lipgloss.NewStyle().PaddingLeft(2).Render(card) + "\n\n" +
			lipgloss.NewStyle().PaddingLeft(2).Render(prompt) + "\n"
	}

	return header + "\n" + body
}

// ═══════════════════════════════════════════════════════════════════════════════
// ── Aliases & preflight ────────────────────────────────────────────────────────
// ═══════════════════════════════════════════════════════════════════════════════

func loadAliases() map[string]string {
	aliases := make(map[string]string)
	home, err := os.UserHomeDir()
	if err != nil {
		return aliases
	}
	f, err := os.Open(filepath.Join(home, ".aws_ssm_aliases"))
	if err != nil {
		return aliases
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := aliasLineRe.FindStringSubmatch(line); len(m) == 3 {
			aliases[m[1]] = m[2]
		}
	}
	return aliases
}

func preflight(profile, region string) (string, error) {
	if region == "" {
		out, err := exec.Command("aws", "configure", "get", "region", "--profile", profile).Output()
		resolved := strings.TrimSpace(string(out))
		if err != nil || resolved == "" {
			return "", fmt.Errorf(
				"could not determine AWS region — set AWS_REGION or configure a default region for profile %q",
				profile,
			)
		}
		region = resolved
	}

	probe := exec.Command("aws", "sts", "get-caller-identity", "--profile", profile)
	probe.Env = cleanEnv()
	if err := probe.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Not logged in. Running: aws sso login --profile %s\n\n", profile)
		login := exec.Command("aws", "sso", "login", "--profile", profile)
		login.Stdin, login.Stdout, login.Stderr = os.Stdin, os.Stdout, os.Stderr
		if runErr := login.Run(); runErr != nil {
			return "", fmt.Errorf("SSO login failed: %w", runErr)
		}
	}

	return region, nil
}

// ═══════════════════════════════════════════════════════════════════════════════
// ── Main ───────────────────────────────────────────────────────────────────────
// ═══════════════════════════════════════════════════════════════════════════════

func main() {
	profile := os.Getenv("AWS_PROFILE")
	region := os.Getenv("AWS_REGION")

	nameArg := ""
	if len(os.Args) > 1 {
		nameArg = os.Args[1]
	}

	// Resolve alias (skip if it looks like a raw instance ID)
	if nameArg != "" && !instanceIDRe.MatchString(nameArg) {
		if resolved, ok := loadAliases()[nameArg]; ok {
			nameArg = resolved
		}
	}

	// ── Profile selection ──────────────────────────────────────────────────────
	// If AWS_PROFILE is not set, show an interactive picker that reads known
	// profiles from ~/.aws/config. The user can also type any arbitrary name.
	if profile == "" {
		selected, err := runProfilePicker(loadProfiles())
		if err != nil {
			// User cancelled
			os.Exit(1)
		}
		profile = selected
	}

	// ── Preflight: resolve region + ensure auth ────────────────────────────────
	resolvedRegion, err := preflight(profile, region)
	if err != nil {
		fmt.Fprintln(os.Stderr, errorStyle.Render("Error: "+err.Error()))
		os.Exit(1)
	}
	if region == "" {
		region = resolvedRegion
	}

	// ── Determine initial instance info ───────────────────────────────────────
	instanceID := ""
	instanceName := nameArg
	if instanceIDRe.MatchString(nameArg) {
		instanceID = nameArg
	}

	// ── Run instance picker + reason TUI ─────────────────────────────────────
	p := tea.NewProgram(
		newModel(profile, region, instanceID, instanceName),
		tea.WithAltScreen(),
	)

	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
		os.Exit(1)
	}

	fm := final.(model)

	if fm.err != nil {
		fmt.Fprintln(os.Stderr, errorStyle.Render("Error: "+fm.err.Error()))
		os.Exit(1)
	}
	if fm.state != stateDone {
		os.Exit(1)
	}

	// ── Build and exec `aws ssm start-session` ────────────────────────────────
	awsBin, err := exec.LookPath("aws")
	if err != nil {
		fmt.Fprintln(os.Stderr, errorStyle.Render("'aws' CLI not found in PATH"))
		os.Exit(1)
	}

	args := []string{
		"aws", "ssm", "start-session",
		"--region", fm.awsRegion,
		"--profile", fm.awsProfile,
		"--target", fm.instanceID,
	}
	if fm.reason != "" {
		args = append(args, "--reason", fm.reason)
	}

	fmt.Println(successStyle.Render("Connecting to " + fm.instanceID + " (" + fm.instanceName + ")..."))

	if err := syscall.Exec(awsBin, args, cleanEnv()); err != nil {
		fmt.Fprintln(os.Stderr, "exec:", err)
		os.Exit(1)
	}
}
