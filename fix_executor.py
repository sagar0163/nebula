import re

with open("internal/agent/executor.go", "r") as f:
    content = f.read()

content = content.replace('import "encoding/json"\npackage agent\n\nimport (\n', 'package agent\n\nimport (\n\t"encoding/json"\n')

with open("internal/agent/executor.go", "w") as f:
    f.write(content)
