with open("internal/agent/executor.go", "r") as f:
    content = f.read()

content = content.replace("return nil, nil, errors.New", "return nil, errors.New")
content = content.replace("return nil, nil, fmt.Errorf", "return nil, fmt.Errorf")
content = content.replace("return nil, nil", "return nil, nil")

with open("internal/agent/executor.go", "w") as f:
    f.write(content)
