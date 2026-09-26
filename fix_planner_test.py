import re

with open("internal/agent/planner_test.go", "r") as f:
    content = f.read()

content = content.replace('\n\t"os"\n\t"path/filepath"', '')

with open("internal/agent/planner_test.go", "w") as f:
    f.write(content)
