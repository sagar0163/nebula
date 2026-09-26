import re

with open("internal/cli/commands.go", "r") as f:
    content = f.read()

content = content.replace(
    '"github.com/sagar0163/nebula/internal/daemon"',
    '"github.com/sagar0163/nebula/internal/daemon"\n\t"github.com/sagar0163/nebula/internal/agent"\n\t"github.com/sagar0163/nebula/internal/safety"'
)

with open("internal/cli/commands.go", "w") as f:
    f.write(content)
