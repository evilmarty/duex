---
name: tui-regression-testing
description: Methodologies for testing interactive terminal applications built with Bubble Tea, including mocking key messages, simulating updates, and asserting rendering states.
---

# TUI Regression Testing Skill

Testing terminal layouts and keyboard interactions requires simulating the standard Bubble Tea lifecycle programmatically. This skill covers techniques for unit testing and asserting Bubble Tea components.

## Simulating Key Event Interactions

To test standard interactions (e.g., navigating a list or canceling a scan):
1. Create an instance of your `Model`.
2. Construct and feed `tea.KeyMsg` structs directly into the model's `Update` function.
3. Assert that the returned model contains the expected state transitions.

### Example Code:
```go
func TestNavigation(t *testing.T) {
	// 1. Initialize model
	m := NewModel("/dummy/path")

	// 2. Simulate Down Arrow key press
	msg := tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'j'},
	}
	
	newModel, cmd := m.Update(msg)
	updated := newModel.(Model)

	// 3. Assert active index changed
	if updated.selectedIndex != 1 {
		t.Errorf("expected selected index to be 1, got %d", updated.selectedIndex)
	}
	
	// 4. Assert a command is returned (if any)
	if cmd == nil {
		t.Log("No active command returned, state updated synchronously.")
	}
}
```

## Asserting View Outputs

To verify what is displayed on the user's terminal:
- Retrieve the output string from the `View()` function.
- Strip ANSI Escape Sequences (colors, borders, cursor indicators) to check raw text, or leave them intact if verifying Lip Gloss styles.

### Helper to Strip ANSI Codes:
```go
import "regexp"

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func StripAnsi(str string) string {
	return ansiPattern.ReplaceAllString(str, "")
}
```

### Writing View Assertions:
```go
func TestViewOutput(t *testing.T) {
	m := NewModel("/test/path")
	m.IsScanning = true

	view := m.View()
	cleanView := StripAnsi(view)

	// Check if the spinner or scan message is rendered
	if !strings.Contains(cleanView, "Scanning") {
		t.Errorf("expected view to contain 'Scanning', got:\n%s", cleanView)
	}
}
```

## Async Command Testing

When an `Update` function returns a `tea.Cmd`, execution happens in the background. To test asynchronous behavior:
- Execute the returned `tea.Cmd` inside your test suite.
- A `tea.Cmd` is a function with the signature `func() tea.Msg`. Call it directly and feed the resulting `tea.Msg` back into the model's `Update` function to complete the state cycle.

```go
func TestAsyncScanStart(t *testing.T) {
	m := NewModel("/test/path")
	
	// Trigger scan
	newModel, cmd := m.Update(triggerScanMsg{})
	
	if cmd != nil {
		// Run command synchronously to fetch the underlying message
		msg := cmd()
		
		// Feed message back to complete the loop
		finalModel, _ := newModel.Update(msg)
		assert.True(t, finalModel.(Model).ScanCompleted)
	}
}
```
