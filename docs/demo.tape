# Terminal demo for the README and docs. Render with:
#   vhs docs/demo.tape        (https://github.com/charmbracelet/vhs)
# Uses only offline commands, so it needs no AWS access.
Output docs/assets/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 24
Set Theme { "background": "#0B1120", "foreground": "#E2E8F0", "cursor": "#FBBF24", "black": "#0F172A", "brightBlack": "#475569", "red": "#F87171", "green": "#4ADE80", "yellow": "#FBBF24", "blue": "#60A5FA", "magenta": "#C084FC", "cyan": "#22D3EE", "white": "#E2E8F0", "brightWhite": "#F8FAFC", "brightRed": "#FCA5A5", "brightGreen": "#86EFAC", "brightYellow": "#FCD34D", "brightBlue": "#93C5FD", "brightMagenta": "#D8B4FE", "brightCyan": "#67E8F9", "selection": "#1E293B" }

Hide
Type "export PS1='$ ' && cd examples && clear"
Enter
Show

Type "envhaul validate"
Sleep 300ms
Enter
Sleep 1.5s

Type "envhaul list api"
Sleep 300ms
Enter
Sleep 4s

Type "envhaul list api --json | jq '.[] | select(.kind == \"template\") | .sources[].path'"
Sleep 300ms
Enter
Sleep 3s
