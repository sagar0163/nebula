import re

with open("internal/agent/agent_test.go", "r") as f:
    content = f.read()

content = content.replace("planner.Plan(ctx, raw, output, transcript)", "planner.Plan(ctx, raw, output, transcript, nil)")
content = content.replace("planner.Plan(ctx, \"broken\", \"output\", \"\")", "planner.Plan(ctx, \"broken\", \"output\", \"\", nil)")
content = content.replace("err = executor.Execute", "_, err = executor.Execute")
content = content.replace("err := executor.Execute", "_, err := executor.Execute")
content = content.replace("buildDiagnosePrompt(cmd, output, transcript)", "buildDiagnosePrompt(cmd, output, transcript, nil)")
content = content.replace("buildDiagnosePrompt(\"ls\", \"\", \"recent ctx\")", "buildDiagnosePrompt(\"ls\", \"\", \"recent ctx\", nil)")
content = content.replace("buildDiagnosePrompt(\"ls\", \"error\", \"\")", "buildDiagnosePrompt(\"ls\", \"error\", \"\", nil)")
content = content.replace("buildDiagnosePrompt(testCmd, fakeOutput, \"\")", "buildDiagnosePrompt(testCmd, fakeOutput, \"\", nil)")

with open("internal/agent/agent_test.go", "w") as f:
    f.write(content)
