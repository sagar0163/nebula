import re

with open("internal/harness/logger.go", "r") as f:
    content = f.read()

old_struct = """type StructuredLogger struct {
	file *os.File
}"""
new_struct = """type StructuredLogger struct {
	file    *os.File
	encoder *json.Encoder
}"""
content = content.replace(old_struct, new_struct)

old_init = """	return &StructuredLogger{file: file}, nil"""
new_init = """	return &StructuredLogger{
		file:    file,
		encoder: json.NewEncoder(file),
	}, nil"""
content = content.replace(old_init, new_init)

old_log = """func (l *StructuredLogger) Log(event LogEvent) error {
	event.Timestamp = time.Now().Format(time.RFC3339)
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	
	_, err = l.file.Write(append(data, '\\n'))
	return err
}"""
new_log = """func (l *StructuredLogger) Log(event LogEvent) error {
	event.Timestamp = time.Now().Format(time.RFC3339)
	return l.encoder.Encode(event)
}"""
content = content.replace(old_log, new_log)

with open("internal/harness/logger.go", "w") as f:
    f.write(content)
