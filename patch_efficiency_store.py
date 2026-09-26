import re

with open("internal/memory/store.go", "r") as f:
    content = f.read()

# Update SavePattern
content = content.replace("success_rate, use_count, embedding", "success_rate, use_count, efficiency_score, embedding")
content = content.replace("VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
content = content.replace("p.SuccessRate, p.UseCount,", "p.SuccessRate, p.UseCount, p.Efficiency,")

# Update FindPattern query
content = content.replace("success_rate, use_count, embedding", "success_rate, use_count, efficiency_score, embedding")
content = content.replace("ORDER BY use_count DESC", "ORDER BY efficiency_score DESC, use_count DESC")

with open("internal/memory/store.go", "w") as f:
    f.write(content)
