import re

with open("internal/models/models.go", "r") as f:
    content = f.read()

content = content.replace('	UseCount    int       `db:"use_count"`', '	UseCount    int       `db:"use_count"`\n\tEfficiency  float64   `db:"efficiency_score"`')

with open("internal/models/models.go", "w") as f:
    f.write(content)
