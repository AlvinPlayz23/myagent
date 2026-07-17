package tools

// DefaultRegistry builds the four core tools (read, write, edit, bash) bound to
// the given working directory, in the canonical order.
func DefaultRegistry(cwd string) *Registry {
	return NewRegistry(
		&ReadTool{Cwd: cwd},
		&WriteTool{Cwd: cwd},
		&EditTool{Cwd: cwd},
		&BashTool{Cwd: cwd},
	)
}
