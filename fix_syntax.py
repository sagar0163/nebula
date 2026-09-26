import re

with open("internal/cli/commands.go", "r") as f:
    content = f.read()

content = content.replace('fmt.Print("\n\t> ")', 'fmt.Print("\\n> ")')
content = content.replace('fmt.Printf("Error: %v\n\t", err)', 'fmt.Printf("Error: %v\\n", err)')

with open("internal/cli/commands.go", "w") as f:
    f.write(content)
