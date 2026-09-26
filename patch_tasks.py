import re

with open("TASK.md", "r") as f:
    content = f.read()

content = content.replace("### TASK-056: Adaptive turn budget — extend to 6 turns on measurable progress\n", "### TASK-056: Adaptive turn budget — extend to 6 turns on measurable progress (DONE)\n")
content = content.replace("### TASK-059: Store and replay multi-turn fix chains\n", "### TASK-059: Store and replay multi-turn fix chains (DONE)\n")

with open("TASK.md", "w") as f:
    f.write(content)
