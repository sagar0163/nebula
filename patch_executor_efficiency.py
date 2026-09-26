import re

with open("internal/agent/executor.go", "r") as f:
    content = f.read()

old_fields = """		SuccessRate: 1.0,
		UseCount:    1,"""
new_fields = """		SuccessRate: 1.0,
		UseCount:    1,
		Efficiency:  1.0 / float64(len(history)+1),"""

content = content.replace(old_fields, new_fields)

with open("internal/agent/executor.go", "w") as f:
    f.write(content)
