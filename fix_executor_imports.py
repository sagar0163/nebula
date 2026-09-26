import re

with open("internal/agent/goal_executor.go", "r") as f:
    content = f.read()

content = content.replace('"os"\n\t"os/exec"\n', "")
content = content.replace('"fmt"\n', '"fmt"\n\t"github.com/sagar0163/nebula/internal/tools"\n')

with open("internal/agent/goal_executor.go", "w") as f:
    f.write(content)
