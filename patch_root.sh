sed -i 's/func loadKeys(baseKey, configVal string) \[\]string {/func loadKeys(baseKey, configVal, envVar string) []string {/' internal/cli/root.go
sed -i 's/add(configVal)/add(configVal)\n\tadd(os.Getenv(envVar))\n\tvar keyringErr error/' internal/cli/root.go
sed -i 's/if k, _ := keyring.Get("nebula", baseKey); k != "" {/if k, err := keyring.Get("nebula", baseKey); err != nil \&\& err != keyring.ErrNotFound {\n\t\tkeyringErr = err\n\t}\n\tif k != "" {/' internal/cli/root.go
sed -i 's/if k, _ := keyring.Get("nebula", baseKey+""+strconv.Itoa(i)); k != "" {/if k, err := keyring.Get("nebula", baseKey+"_"+strconv.Itoa(i)); err != nil \&\& err != keyring.ErrNotFound {\n\t\t\tkeyringErr = err\n\t\t}\n\t\tif k != "" {/' internal/cli/root.go
sed -i 's/return keys/\n\tif len(keys) == 0 \&\& keyringErr != nil {\n\t\tfmt.Fprintf(os.Stderr, "nebula: keyring unavailable (%v) — set %s or run nebula key add\\n", keyringErr, envVar)\n\t}\n\n\treturn keys/' internal/cli/root.go
