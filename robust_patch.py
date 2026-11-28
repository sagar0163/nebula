import sys

with open("internal/harness/orchestrator.go", "r") as f:
    lines = f.readlines()

new_lines = []
skip_critic_added = False

for i, line in enumerate(lines):
    if "execStart := time.Now()" in line:
        new_lines.append(line)
        continue
        
    if "execOutput, err := o.executor.Execute(ctx, execCtx)" in line and "execStart" in lines[i-1]:
        # Replace this line with the multi-model logic
        indent = line[:len(line) - len(line.lstrip())]
        logic = f"""{indent}var execOutput shared.AgentOutput
{indent}var err error
{indent}var skipCritic bool
{indent}
{indent}if o.MultiModel {{
{indent}	fmt.Println("Running multi-model consensus check...")
{indent}	out1, err1 := o.executor.Execute(ctx, execCtx)
{indent}	out2, err2 := o.executor.Execute(ctx, execCtx)
{indent}	
{indent}	if err1 == nil && err2 == nil {{
{indent}		if out1.Context == out2.Context {{
{indent}			fmt.Println("Models reached consensus! Will skip Critic.")
{indent}			execOutput = out1
{indent}			skipCritic = true
{indent}		}} else {{
{indent}			fmt.Println("Models diverged. Proceeding to Critic for tie-breaking...")
{indent}			execOutput = out1
{indent}		}}
{indent}	}} else {{
{indent}		execOutput, err = out1, err1
{indent}	}}
{indent}}} else {{
{indent}	execOutput, err = o.executor.Execute(ctx, execCtx)
{indent}}}
"""
        new_lines.append(logic)
        continue
        
    if "fmt.Println(\"=== PHASE 4: CRITIQUE ===\")" in line:
        indent = line[:len(line) - len(line.lstrip())]
        new_lines.append(f"{indent}if skipCritic {{\n")
        new_lines.append(f"{indent}\tfmt.Println(\"=== PHASE 4: CRITIQUE (SKIPPED DUE TO CONSENSUS) ===\")\n")
        new_lines.append(f"{indent}}} else {{\n")
        new_lines.append(line)
        skip_critic_added = True
        continue
        
    if skip_critic_added and "fmt.Println(\">> Critic approved the patch.\")" in line:
        new_lines.append(line)
        indent = line[:len(line) - len(line.lstrip())]
        new_lines.append(f"{indent[:-1]}}}\n") # close the else block
        skip_critic_added = False
        continue

    new_lines.append(line)

with open("internal/harness/orchestrator.go", "w") as f:
    f.writelines(new_lines)
