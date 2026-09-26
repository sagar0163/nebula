import re

with open("internal/memory/store.go", "r") as f:
    content = f.read()

content = content.replace("INSERT INTO commands (session_id, raw, exit_code, stdout, stderr, elapsed_ms, work_dir, created_at)\n\t\tVALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", "INSERT INTO commands (session_id, raw, exit_code, stdout, stderr, elapsed_ms, work_dir, created_at)\n\t\tVALUES (?, ?, ?, ?, ?, ?, ?, ?)")

with open("internal/memory/store.go", "w") as f:
    f.write(content)
