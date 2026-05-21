---
name: charm-tui-development
description: Guidelines and best practices for building robust and responsive Terminal User Interfaces (TUIs) using the Charm ecosystem (Bubble Tea, Bubbles, Lip Gloss) in Go.
---

# Charm TUI Development Skill

This skill provides comprehensive instructions, architectural conventions, and code patterns for developing high-quality, jitter-free terminal interfaces using Bubble Tea, Bubbles, and Lip Gloss.

## Core Architecture (Elm/MVU Pattern)

Bubble Tea is built on the Model-View-Update architecture. To ensure consistency and prevent rendering bugs:

1. **State Mutation Boundary**: All model state mutations (e.g., resizing, updating list elements, updating spinners) must happen exclusively in the `Update` loop.
   > [!CAUTION]
   > Do **NOT** mutate the model or perform state adjustments in the `View` function. The `View` method receives a copy/receiver of the model, so any changes made inside it will be lost on subsequent render ticks.

2. **Dimension Updates**: When a `tea.WindowSizeMsg` is received, propagate the dimensions to all sub-components using their respective `SetSize` or `SetHeight` methods within the `Update` handler.

## Visual Design & Aesthetics

Follow these principles when styling with Lip Gloss to achieve a premium, modern terminal aesthetic:

- **Harmonious Palette**: Define a strict color scheme (e.g., in `pkg/ui/styles.go`) using curated HSL/Hex values rather than standard terminal colors.
- **Dynamic Highlights**: Use vibrant accent colors for focused items or active cursor positions, and muted colors for inactive/background text.
- **Borders & Paddings**: Apply subtle rounded borders (`lipgloss.RoundedBorder()`) and consistent paddings to group relevant information.

## Robust Rendering & Jitter Prevention

Terminal jitter occurs when the height/width of rendered outputs fluctuates rapidly (e.g., during asynchronous loading ticks).

### Rules for Jitter-Free Views:
- **Unified Output String**: Always return a single, unified string from the `View` function.
- **Fixed Heights**: Assign static or container-bounded heights to header, body, and footer panels.
- **Vertical Joining**: Use `lipgloss.JoinVertical` or `lipgloss.JoinHorizontal` to align panels precisely.
- **Line Truncation**: Explicitly truncate lines that exceed the terminal width to prevent unwanted wrapping.

### Example View Structure:
```go
func (m Model) View() string {
    // 1. Render components
    header := m.header.View()
    body := m.body.View()
    footer := m.footer.View()
    
    // 2. Join vertically with consistent alignment
    return lipgloss.JoinVertical(
        lipgloss.Left,
        header,
        body,
        footer,
    )
}
```

## Custom List Delegates

When using the `bubbles/list` component, custom delegates allow you to render item-specific metadata and visual states.

- Always implement the `list.ItemDelegate` interface.
- Ensure the delegate handles different rendering contexts (e.g., normal, selected, matched filter).
- Maintain responsiveness by delegating CPU-intensive formatting outside the immediate render loop.
- **Emoji Layout & Cell Widths**: Emojis (e.g., `⚠️ `) often occupy more than 1 byte but represent a specific number of visual cells in the terminal (typically 3 visual columns including a trailing space). Explicitly account for this visual cell width (instead of raw character length) when calculating list item margins, truncate lengths, and dynamic column alignments to prevent layout shift or visual jitter.
- **Style Isolation for Compound Elements**: When highlighting selected list items, style each compound sub-component individually (e.g., `warnStyle.Render("⚠️ ") + selectedStyle.Render(text)`) rather than wrapping the entire concatenated block. This guarantees robust text attribute rendering (like bolding or background highlighting) and avoids rendering limitations in terminal emulators.


## Context-Aware Help Component

Use Charm's `bubbles/help` to present keyboard shortcuts dynamically based on the current application state.

1. Define a struct implementing `help.KeyMap`.
2. Categorize keys into `ShortHelp` (always visible) and `FullHelp` (shown when expanding with `?`).
3. Update the key map dynamically if the user shifts contexts (e.g., from loading -> browsing -> filtering).
