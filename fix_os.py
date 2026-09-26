import re

with open("internal/agent/goal_executor.go", "r") as f:
    content = f.read()

content = content.replace('"fmt"\n', '"fmt"\n\t"os"\n')

with open("internal/agent/goal_executor.go", "w") as f:
    f.write(content)
