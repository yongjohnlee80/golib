package widget

// tui/widget forwards every name tui/widget/core exports (core_facade.go, generated), so a program
// importing tui/widget compiles unchanged while golib's widgets move into packages of their own.
// TestTheFacadeIsGenerated fails when core exports a name the facade does not forward yet.

//go:generate go run ./internal/facadegen/cmd/facadegen
