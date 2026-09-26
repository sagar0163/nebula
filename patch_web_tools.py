import re

with open("internal/agent/goal_executor.go", "r") as f:
    content = f.read()

content = content.replace("tr.Register(&tools.GitTool())", "tr.Register(&tools.GitTool())\n\ttr.Register(&tools.WebFetchTool())\n\ttr.Register(&tools.WebSearchTool())")

with open("internal/agent/goal_executor.go", "w") as f:
    f.write(content)
