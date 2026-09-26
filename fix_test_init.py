import re

with open("internal/agent/agent_test.go", "r") as f:
    content = f.read()

bad_init = """	harness := pty.NewHarness()
	store := memory.NewMockStore()
	router := llm.NewRouter(store)
	executor := NewExecutor(harness, router, store)"""
good_init = """	harness, _ := pty.NewHarness(80, 24)
	store := newTestStore(t)
	router := llm.NewRouter()
	executor := NewExecutor(harness, router, store)"""

content = content.replace(bad_init, good_init)

with open("internal/agent/agent_test.go", "w") as f:
    f.write(content)
