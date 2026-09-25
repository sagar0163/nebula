import re

def fix_file(filename):
    with open(filename, "r") as f:
        content = f.read()

    # Plan
    content = re.sub(r'planner\.Plan\(([^,]+, [^,]+, [^,]+, [^)]+)\)', r'planner.Plan(\1, nil)', content)
    # buildDiagnosePrompt
    content = re.sub(r'buildDiagnosePrompt\(([^,]+, [^,]+, [^)]+)\)', r'buildDiagnosePrompt(\1, nil)', content)
    # executor.Execute
    content = re.sub(r'(\s+)err = executor\.Execute', r'\1_, err = executor.Execute', content)
    content = re.sub(r'(\s+)err := executor\.Execute', r'\1_, err := executor.Execute', content)

    with open(filename, "w") as f:
        f.write(content)

fix_file("internal/agent/agent_test.go")
fix_file("internal/agent/executor_meta_test.go")
