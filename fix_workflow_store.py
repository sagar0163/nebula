import re

with open("internal/memory/store.go", "r") as f:
    content = f.read()

content = content.replace("INSERT INTO workflow_jobs (id, workflow_file, inputs, status, current_step, output, error, created_at, updated_at)\n\t\tVALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", "INSERT INTO workflow_jobs (id, workflow_file, inputs, status, current_step, output, error, created_at, updated_at)\n\t\tVALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)")

with open("internal/memory/store.go", "w") as f:
    f.write(content)
