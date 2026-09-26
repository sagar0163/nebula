import re

with open("TASK.md", "r") as f:
    content = f.read()

content = content.replace("### TASK-060: Confidence-gated execution — approval threshold scales with confidence\n", "### TASK-060: Confidence-gated execution — approval threshold scales with confidence (DONE)\n")
content = content.replace("### TASK-061: Fix quality scoring — prefer first-attempt patterns on recall\n", "### TASK-061: Fix quality scoring — prefer first-attempt patterns on recall (DONE)\n")

with open("TASK.md", "w") as f:
    f.write(content)
