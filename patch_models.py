import re

with open("internal/models/models.go", "r") as f:
    content = f.read()

content = content.replace('	Embedding   []byte    `db:"embedding"` // float32 slice, gob-encoded', '	Embedding   []byte    `db:"embedding"` // float32 slice, gob-encoded\n\tFixChain    string    `db:"fix_chain"`')

with open("internal/models/models.go", "w") as f:
    f.write(content)

with open("internal/memory/store.go", "r") as f:
    content = f.read()

content = content.replace("embedding, created_at, updated_at", "embedding, fix_chain, created_at, updated_at")
content = content.replace("embedding, created_at, updated_at)", "embedding, fix_chain, created_at, updated_at)")
content = content.replace("VALUES (?, ?, ?, ?, ?, ?, ?, ?)`", "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`")
content = content.replace("p.Embedding, p.CreatedAt, p.UpdatedAt,", "p.Embedding, p.FixChain, p.CreatedAt, p.UpdatedAt,")

with open("internal/memory/store.go", "w") as f:
    f.write(content)

